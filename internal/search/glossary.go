// Package search contains query understanding that is independent from the
// storage layer: language detection, glossary expansion and (later) ranking.
package search

import (
	"strings"
	"unicode"
)

// glossary maps Spanish programming terms to the English words used by MDN.
// It is intentionally small and curated: a handful of good synonyms beats a
// huge list of noisy ones.
var glossary = map[string][]string{
	"objeto":       {"object"},
	"objetos":      {"object"},
	"copiar":       {"copy", "clone"},
	"copio":        {"copy", "clone"},
	"copias":       {"copy", "clone"},
	"copiamos":     {"copy", "clone"},
	"copia":        {"copy", "clone"},
	"clonar":       {"clone"},
	"clonacion":    {"clone"},
	"mutar":        {"mutate", "mutation"},
	"mutarlo":      {"mutate", "mutation"},
	"muta":         {"mutate", "mutation"},
	"mutacion":     {"mutation", "mutate"},
	"cambiar":      {"change"},
	"cambio":       {"change"},
	"arreglo":      {"array"},
	"arreglos":     {"array"},
	"lista":        {"array", "list"},
	"listas":       {"array", "list"},
	"bucle":        {"loop", "iterate"},
	"bucles":       {"loop", "iterate"},
	"iterar":       {"iterate"},
	"iteracion":    {"iteration", "iterate"},
	"recorrer":     {"iterate", "loop"},
	"promesa":      {"promise"},
	"promesas":     {"promise"},
	"asincrono":    {"async", "asynchronous"},
	"asincronia":   {"async", "asynchronous"},
	"esperar":      {"await", "wait"},
	"funcion":      {"function"},
	"funciones":    {"function"},
	"metodo":       {"method"},
	"metodos":      {"method"},
	"propiedad":    {"property"},
	"propiedades":  {"property"},
	"clase":        {"class"},
	"clases":       {"class"},
	"instancia":    {"instance"},
	"constructor":  {"constructor"},
	"herencia":     {"inheritance", "inherit"},
	"prototipo":    {"prototype"},
	"cadena":       {"string"},
	"cadenas":      {"string"},
	"texto":        {"string", "text"},
	"numero":       {"number"},
	"numeros":      {"number"},
	"booleano":     {"boolean"},
	"nulo":         {"null"},
	"indefinido":   {"undefined"},
	"variable":     {"variable"},
	"variables":    {"variable"},
	"constante":    {"constant", "const"},
	"declarar":     {"declare", "declaration"},
	"declaracion":  {"declaration"},
	"ambito":       {"scope"},
	"alcance":      {"scope"},
	"devolver":     {"return"},
	"retornar":     {"return"},
	"retorno":      {"return"},
	"parametro":    {"parameter"},
	"parametros":   {"parameter"},
	"argumento":    {"argument"},
	"argumentos":   {"argument"},
	"llamar":       {"call", "invoke"},
	"invocar":      {"invoke", "call"},
	"crear":        {"create"},
	"eliminar":     {"delete", "remove"},
	"borrar":       {"delete", "remove"},
	"agregar":      {"add", "append"},
	"anadir":       {"add", "append"},
	"insertar":     {"insert"},
	"obtener":      {"get"},
	"establecer":   {"set"},
	"convertir":    {"convert", "cast"},
	"comparar":     {"compare"},
	"igualdad":     {"equality"},
	"estricto":     {"strict"},
	"error":        {"error"},
	"errores":      {"error"},
	"excepcion":    {"exception", "throw"},
	"lanzar":       {"throw"},
	"capturar":     {"catch"},
	"evento":       {"event"},
	"eventos":      {"event"},
	"escuchar":     {"listen"},
	"temporizador": {"timer"},
	"retraso":      {"delay", "timeout"},
	"intervalo":    {"interval"},
	"peticion":     {"request", "fetch"},
	"solicitud":    {"request", "fetch"},
	"respuesta":    {"response"},
	"servidor":     {"server"},
	"red":          {"network"},
	"archivo":      {"file"},
	"fichero":      {"file"},
	"leer":         {"read"},
	"escribir":     {"write"},
	"flujo":        {"stream"},
	"mapa":         {"map"},
	"conjunto":     {"set"},
	"cola":         {"queue"},
	"pila":         {"stack"},
	"ordenar":      {"sort"},
	"filtrar":      {"filter"},
	"reducir":      {"reduce"},
	"buscar":       {"find", "search"},
	"incluir":      {"includes", "include"},
	"contener":     {"contains"},
	"dividir":      {"split", "slice"},
	"unir":         {"join", "concat"},
	"concatenar":   {"concat"},
	"indice":       {"index"},
	"longitud":     {"length"},
	"tamano":       {"size"},
	"clave":        {"key"},
	"valor":        {"value"},
	"entrada":      {"entry", "input"},
	"salida":       {"output"},
	"profundo":     {"deep"},
	"superficial":  {"shallow"},
	"inmutable":    {"immutable"},
	"mutable":      {"mutable"},
	"referencia":   {"reference"},
	"memoria":      {"memory"},
	"modulo":       {"module"},
	"modulos":      {"module"},
	"importar":     {"import"},
	"exportar":     {"export"},
	"paquete":      {"package"},
	"dependencia":  {"dependency"},
	"version":      {"version"},
	"entorno":      {"environment"},
	"navegador":    {"browser"},
	"consola":      {"console"},
	"registro":     {"log"},
	"depurar":      {"debug"},
	"prueba":       {"test"},
	"probar":       {"test"},
	"documento":    {"document"},
	"ventana":      {"window"},
	"elemento":     {"element"},
	"elementos":    {"element"},
	"atributo":     {"attribute"},
	"estilo":       {"style"},
	"mientras":     {"while"},
	"caso":         {"case"},
	"romper":       {"break"},
	"continuar":    {"continue"},
	"nuevo":        {"new"},
	"verdadero":    {"true"},
	"falso":        {"false"},
	"fecha":        {"date"},
	"hora":         {"time"},
	"tiempo":       {"time"},
	"matriz":       {"array"},
	"matrices":     {"array"},
}

var stopwords = map[string]bool{
	"a": true, "al": true, "algo": true, "como": true, "con": true,
	"cual": true, "cuando": true, "de": true, "del": true, "donde": true,
	"el": true, "en": true, "entre": true, "es": true, "esa": true,
	"ese": true, "eso": true, "esta": true, "este": true, "esto": true,
	"hay": true, "la": true, "las": true, "le": true, "les": true,
	"lo": true, "los": true, "mas": true, "me": true, "mi": true,
	"mucho": true, "muy": true, "no": true, "nos": true, "o": true,
	"para": true, "pero": true, "por": true, "porque": true, "que": true,
	"se": true, "ser": true, "si": true, "sin": true, "sobre": true,
	"son": true, "su": true, "te": true, "tu": true, "un": true,
	"una": true, "uno": true, "y": true, "ya": true,
}

// ExpandQuery turns a Spanish (or mixed) question into an FTS5 expression
// whose groups are ANDed. The second return value reports whether any glossary
// term was applied; when it is false the caller should keep its normal
// English-oriented query.
func ExpandQuery(query string) (string, bool) {
	groups, expanded := groupsFor(query)
	if len(groups) == 0 {
		return "", false
	}
	return strings.Join(groups, " AND "), expanded
}

// ExpandQueryLoose is the relaxed variant: OR between groups. It is used as a
// fallback when the strict query finds nothing, which is common for free-form
// Spanish questions with words that are not in the glossary.
func ExpandQueryLoose(query string) (string, bool) {
	groups, expanded := groupsFor(query)
	if len(groups) == 0 {
		return "", false
	}
	return strings.Join(groups, " OR "), expanded
}

func groupsFor(query string) ([]string, bool) {
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil, false
	}

	var (
		groups   []string
		expanded bool
	)
	for _, tok := range tokens {
		if stopwords[tok] {
			continue
		}
		terms := []string{tok}
		if en := lookup(tok); len(en) > 0 {
			terms = append(terms, en...)
			expanded = true
		}

		quoted := make([]string, 0, len(terms))
		seen := map[string]bool{}
		for _, t := range terms {
			if t == "" || seen[t] {
				continue
			}
			seen[t] = true
			quoted = append(quoted, quote(t))
		}
		if len(quoted) > 0 {
			groups = append(groups, "("+strings.Join(quoted, " OR ")+")")
		}
	}
	return groups, expanded
}

// sharedSpanishEnglish lists glossary terms that are spelled the same in
// English. They are weak evidence: "variable" or "error" alone must not turn
// an English query into Spanish.
var sharedSpanishEnglish = map[string]bool{
	"error": true, "constructor": true, "variable": true, "variables": true,
	"mutable": true, "immutable": true, "version": true, "red": true,
	"superficial": true, "matrices": true,
}

// spanishSignals scores words that are characteristic of Spanish. Strong
// signals are Spanish-only words; weak ones also appear in English, in
// identifiers, or are too short to decide on their own.
var spanishSignals = map[string]int{
	"como": 3, "cual": 3, "cuales": 3, "cuando": 3, "donde": 3,
	"entre": 3, "para": 3, "porque": 3, "pero": 3, "sobre": 3,
	"sin": 3, "desde": 3, "hasta": 3, "tambien": 3, "tampoco": 3,
	"diferencia": 3, "diferencias": 3, "ejemplo": 3, "ejemplos": 3,
	"explicar": 3, "explica": 3, "funciona": 3, "funcionan": 3,
	"usar": 3, "utilizar": 3, "hacer": 3, "puedo": 3, "puede": 3,
	"quiero": 3, "necesito": 3, "sirve": 3, "significa": 3,
	"trabajar": 3, "trabaja": 3, "mucho": 3, "muy": 3, "algo": 3,
	"esto": 3, "esta": 3, "este": 3, "esa": 3, "ese": 3, "eso": 3,
	"uno": 3, "una": 3, "unos": 3, "unas": 3, "un": 2,

	// Weak: also common in English, in identifiers, or too short to decide.
	"a": 1, "o": 1, "y": 1, "e": 1, "u": 1, "no": 1, "si": 1,
	"me": 1, "te": 1, "se": 1, "su": 1, "tu": 1, "mi": 1, "lo": 1,
	"le": 1, "les": 1, "nos": 1, "son": 1, "mas": 1, "ya": 1, "hay": 1,

	// Common Spanish function words. They are weak on their own but combine
	// with identifiers such as "array" or "map" without flipping to English.
	"de": 2, "del": 2, "el": 2, "la": 2, "los": 2, "las": 2, "al": 2,
	"en": 2, "es": 2, "que": 2, "con": 2, "por": 2,
}

// englishWords are common English words and JavaScript terms. They push the
// score down so a mixed query such as "how do I copy an object" stays English.
var englishWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "not": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true,
	"do": true, "does": true, "did": true, "i": true, "you": true, "we": true,
	"they": true, "it": true, "to": true, "of": true, "in": true, "on": true,
	"at": true, "for": true, "with": true, "without": true, "from": true,
	"by": true, "as": true, "this": true, "that": true, "these": true,
	"those": true, "there": true, "here": true, "can": true, "could": true,
	"should": true, "would": true, "will": true, "shall": true, "may": true,
	"might": true, "must": true, "have": true, "has": true, "had": true,
	"get": true, "make": true, "use": true, "using": true, "used": true,
	"work": true, "works": true, "working": true, "need": true, "want": true,
	"know": true, "see": true, "look": true, "find": true, "search": true,
	"explain": true, "example": true, "examples": true, "difference": true,
	"differences": true, "between": true, "why": true, "when": true,
	"where": true, "which": true, "who": true, "whose": true, "how": true,
	"what": true, "copy": true, "clone": true, "object": true, "objects": true,
	"array": true, "arrays": true, "function": true, "functions": true,
	"method": true, "methods": true, "property": true, "properties": true,
	"class": true, "classes": true, "instance": true, "instances": true,
	"inheritance": true, "prototype": true, "string": true, "strings": true,
	"number": true, "numbers": true, "boolean": true, "null": true,
	"undefined": true, "declare": true, "declaration": true, "scope": true,
	"return": true, "parameter": true, "parameters": true, "argument": true,
	"arguments": true, "call": true, "invoke": true, "create": true,
	"delete": true, "remove": true, "add": true, "append": true, "insert": true,
	"convert": true, "compare": true, "equality": true, "strict": true,
	"throw": true, "catch": true, "event": true, "events": true, "listen": true,
	"timer": true, "delay": true, "interval": true, "request": true,
	"response": true, "server": true, "network": true, "file": true,
	"files": true, "read": true, "write": true, "stream": true, "queue": true,
	"stack": true, "sort": true, "filter": true, "reduce": true, "includes": true,
	"include": true, "contains": true, "split": true, "slice": true, "join": true,
	"concat": true, "index": true, "length": true, "size": true, "key": true,
	"value": true, "entry": true, "input": true, "output": true, "deep": true,
	"shallow": true, "reference": true, "memory": true, "module": true,
	"modules": true, "import": true, "export": true, "package": true,
	"dependency": true, "environment": true, "browser": true, "console": true,
	"log": true, "debug": true, "test": true, "tests": true, "document": true,
	"window": true, "element": true, "elements": true, "attribute": true,
	"style": true, "while": true, "case": true, "break": true, "continue": true,
	"new": true, "true": true, "false": true, "date": true, "time": true,
	"loop": true, "iterate": true, "iteration": true, "async": true,
	"asynchronous": true, "await": true, "wait": true, "promise": true,
	"promises": true, "mutate": true, "mutation": true, "change": true,
	"list": true, "lists": true, "handling": true, "handlers": true,
	"handler": true, "maps": true, "filters": true, "sets": true,
	"errors": true, "values": true, "keys": true, "entries": true,
	"inputs": true, "outputs": true, "types": true, "type": true,
	"data": true, "code": true,
}

// IsSpanish reports whether the query looks Spanish. It combines weighted
// signals instead of accepting the first stopword: accents and Spanish-only
// words count strongly, ambiguous words such as "a" count weakly, and common
// English words count against Spanish.
func IsSpanish(query string) bool {
	// Accents, ñ and Spanish opening punctuation are decisive on their own;
	// English technical terms must not cancel them out.
	for _, r := range strings.ToLower(query) {
		switch r {
		case 'á', 'é', 'í', 'ó', 'ú', 'ü', 'ñ', '¿', '¡':
			return true
		}
	}

	score := 0
	hasEnglish := false
	for _, t := range tokenize(query) {
		switch {
		case sharedSpanishEnglish[t]:
			score++
		case spanishSignals[t] > 0:
			score += spanishSignals[t]
		case englishWords[t]:
			hasEnglish = true
		case len(lookup(t)) > 0:
			score += 3
		}
	}
	// English words act as a single penalty, not one per token: a Spanish
	// question can legitimately contain several identifiers such as
	// "Array.prototype.map".
	if hasEnglish {
		score -= 2
	}
	return score >= 3
}

func lookup(tok string) []string {
	if en, ok := glossary[tok]; ok {
		return en
	}
	// Cheap singular fallback: objetos -> objeto, funciones -> funcion.
	for _, suffix := range []string{"es", "s"} {
		if strings.HasSuffix(tok, suffix) && len(tok) > len(suffix)+2 {
			if en, ok := glossary[strings.TrimSuffix(tok, suffix)]; ok {
				return en
			}
		}
	}
	return nil
}

func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, stripAccents(f))
	}
	return out
}

func stripAccents(s string) string {
	return strings.NewReplacer(
		"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u",
		"ñ", "n", "à", "a", "è", "e", "ì", "i", "ò", "o", "ù", "u",
	).Replace(s)
}

func quote(term string) string {
	return `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
}
