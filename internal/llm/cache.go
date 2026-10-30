package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const cacheKeyVersion = "v2"

// Cache stores answers on disk so repeated questions do not spend free-tier
// quota. One JSON file per answer keeps it simple and safe to delete.
type Cache struct {
	Dir string
}

func NewCache(dir string) *Cache { return &Cache{Dir: dir} }

// Stats summarizes the answer cache.
type Stats struct {
	Answers   int
	Bytes     int64
	Oldest    time.Time
	Newest    time.Time
	HintShown bool
}

// ClearResult reports what was removed from the cache.
type ClearResult struct {
	Answers     int
	Bytes       int64
	HintRemoved bool
}

// Key identifies an answer by question, model, language and the exact chunks
// used as context. Changing any of them, including the body of a chunk,
// produces a different entry.
func Key(question, model, answerLang string, chunks []Chunk) string {
	h := sha256.New()
	writeField := func(s string) {
		fmt.Fprintf(h, "%d:%s", len(s), s)
	}
	writeField(cacheKeyVersion)
	writeField(promptVersion)
	writeField(model)
	writeField(answerLang)
	writeField(question)
	for _, c := range chunks {
		fmt.Fprintf(h, "%d:", c.N)
		writeField(c.Symbol)
		writeField(c.Kind)
		writeField(c.Heading)
		writeField(c.URL)
		writeField(c.Body)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (c *Cache) answersDir() string {
	return filepath.Join(c.Dir, "answers")
}

func (c *Cache) hintPath() string {
	return filepath.Join(c.Dir, "llm-hint")
}

func (c *Cache) path(key string) string {
	return filepath.Join(c.answersDir(), key[:2], key+".json")
}

func (c *Cache) Get(key string) (Answer, bool) {
	data, err := os.ReadFile(c.path(key))
	if err != nil {
		return Answer{}, false
	}
	var a Answer
	if err := json.Unmarshal(data, &a); err != nil {
		return Answer{}, false
	}
	if strings.TrimSpace(a.Text) == "" {
		return Answer{}, false
	}
	return a, true
}

func (c *Cache) Put(key string, a Answer) error {
	p := c.path(key)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".answer-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, p)
}

// Stats counts answer files and reports their size and age range.
func (c *Cache) Stats() Stats {
	var st Stats
	_ = filepath.WalkDir(c.answersDir(), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		st.Answers++
		st.Bytes += info.Size()
		mtime := info.ModTime()
		if st.Oldest.IsZero() || mtime.Before(st.Oldest) {
			st.Oldest = mtime
		}
		if st.Newest.IsZero() || mtime.After(st.Newest) {
			st.Newest = mtime
		}
		return nil
	})
	if _, err := os.Stat(c.hintPath()); err == nil {
		st.HintShown = true
	}
	return st
}

// Clear removes the answer cache and the one-time LLM hint marker. Files that
// are not part of those known paths are left untouched.
func (c *Cache) Clear() (ClearResult, error) {
	st := c.Stats()
	result := ClearResult{Answers: st.Answers, Bytes: st.Bytes}
	if err := os.RemoveAll(c.answersDir()); err != nil {
		return result, err
	}
	if err := os.Remove(c.hintPath()); err == nil {
		result.HintRemoved = true
	} else if !os.IsNotExist(err) {
		return result, err
	}
	return result, nil
}
