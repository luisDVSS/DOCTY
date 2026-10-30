# Docty

CLI en Go para consultar documentación de programación desde la terminal, por
símbolo o en lenguaje natural. La documentación se indexa localmente: las
consultas funcionan sin conexión. La síntesis con un modelo abierto es opcional
y usa la red.

v1 trabaja solo con JavaScript y usa el contenido de
[mdn/content](https://github.com/mdn/content) (CC BY-SA 2.5).

## Estado

| Área | Estado |
|---|---|
| Ingesta de MDN (frontmatter, macros, chunking) | ✅ |
| Índice SQLite + FTS5 | ✅ |
| Lookup exacto y por nombre corto (`array.map`, `map`) | ✅ |
| Búsqueda textual, desambiguación y sugerencias | ✅ |
| Glosario ES→EN y consultas en español | ✅ |
| Síntesis con LLM (Groq / OpenRouter / compatible) | ✅ |
| Caché de respuestas y salida `--json` | ✅ |
| Paginador y fallback sin API key | ✅ |
| Descarga del snapshot (`docty update`) y bootstrap local | ✅ |
| Streaming token a token | ✅ |
| Keyring del sistema (`docty auth`) | ✅ |
| Gestión de configuración (`docty config`) | ✅ |
| Gestión de caché (`docty cache`) | ✅ |
| Salida compacta (`--simple` / `-s`) | ✅ |
| TUI | ⏳ |

## Instalación

```bash
make build          # bin/docty
make install        # ~/.local/bin/docty
```

`make install` avisa si el directorio de instalación no está en el `PATH`.
Para instalarlo en otro sitio:

```bash
make install INSTALL_DIR="$HOME/go/bin"
```

Requiere Go 1.26+.

## Primeros pasos

Descarga el snapshot de JavaScript y construye el índice:

```bash
docty update js
docty js Array.prototype.map
docty js array.map
docty js map
docty js "spread syntax"
docty -s js Array.prototype.map   # versión compacta
```

`update` hace un checkout disperso de `mdn/content` (solo las páginas de
JavaScript), fija el commit del manifest para que la ingesta sea reproducible y
reconstruye el índice. Necesita `git` y conexión; el resto de consultas
funcionan sin red. Para traer la última versión del upstream:

```bash
docty update js --ref main
```

El índice se guarda en `~/.local/share/docty/index.db` (unos 16 MB para todo
JavaScript).

Si ya tienes un checkout de MDN, puedes saltarte la descarga:

```bash
docty index js --from /tmp/mdn
```

La primera consulta sin índice pero con snapshot local lo construye
automáticamente.

## Comandos

```text
docty [lang] <consulta>        consulta por símbolo, texto o pregunta
docty ask [lang] <pregunta>    responde con el LLM usando fragmentos citados
docty get [lang] <símbolo>     fuerza la búsqueda exacta
docty search [lang] <términos> fuerza la búsqueda textual (sin LLM)
docty list [lang]              lista los símbolos indexados
docty update [lang]            descarga el snapshot y reconstruye el índice
docty index [lang] --from DIR  construye el índice desde un snapshot local
docty ingest [lang] --from DIR --dump <símbolo>
                               muestra el Markdown normalizado (debug)
docty sources                  fuentes conocidas y su estado
docty doctor [--online]        diagnóstico de la instalación
docty auth login               guarda la API key en el keyring y configura el proveedor
docty auth status [--online]   muestra el proveedor y el origen de la clave
docty auth logout              borra la API key del keyring
docty config [show]            muestra la configuración efectiva y su origen
docty config get <clave>       muestra el valor efectivo de una clave
docty config set <clave> <v>   escribe una clave en config.toml
docty config unset <clave>     elimina una clave de config.toml
docty config path              muestra la ruta de config.toml
docty cache [stats]            muestra el tamaño y la antigüedad de la caché
docty cache clear              elimina las respuestas y el aviso único del LLM
docty cache path               muestra la ruta del directorio de caché
docty version
```

Flags globales:

| Flag | Efecto |
|---|---|
| `--json` | salida JSON estable (`schema_version: 1`) |
| `--lang` | lenguaje por defecto |
| `--no-llm` | no sintetizar; solo fragmentos |
| `--require-llm` | falla con exit 4 si el LLM no está disponible |
| `--sources` | muestra los fragmentos enviados al modelo |
| `--brief` | solo la respuesta, sin fuentes |
| `--simple`, `-s` | salida compacta: resumen y sintaxis, menos fragmentos y fuentes citadas resumidas |
| `--refresh` | ignora la caché de respuestas |
| `--stream` | streaming del LLM: `auto` (TTY), `always` o `never` |
| `--no-pager` | no usar `less` |
| `--no-color` | desactiva los estilos ANSI (mantiene el renderizado) |
| `--config` | ruta alternativa a `config.toml` |
| `--verbose` | diagnóstico en stderr |

`--simple` solo cambia la salida humana; con `--json` el esquema y el contenido
estructurado no se recortan. En `ask`, `--simple` muestra las fuentes citadas de
forma compacta y `--brief` las omite por completo.

`format` controla la presentación: `auto` usa la salida humana en un terminal y
Markdown plano en tuberías; `tty` fuerza la salida humana; `md` fuerza Markdown;
`json` equivale a `--json`. En el modo TTY, Docty renderiza el Markdown de las
páginas y de las respuestas: encabezados, negritas, código, listas, citas y
enlaces con su URL. `--no-color` desactiva los estilos ANSI pero mantiene la
estructura renderizada; `NO_COLOR` hace lo mismo desde el entorno. Las tuberías
siguen recibiendo Markdown limpio.

Con `--stream=auto` (por defecto) la respuesta del modelo se escribe según llega
si stdout es un terminal. El renderizador ANSI procesa el stream por líneas, así
que la respuesta sigue apareciendo de forma incremental. En tuberías, con
`--json` o con `--stream=never` se espera la respuesta completa. Si el stream se
interrumpe, se conserva lo ya recibido, se avisa por stderr y el proceso termina
con exit 3; las respuestas incompletas no se guardan en caché.

### Ejemplos

```bash
# lookup exacto (con alias Array.map → Array.prototype.map)
docty js array.map

# desambiguación por nombre corto
docty js map

# salida compacta: resumen, sintaxis y URL
docty -s js Array.prototype.map
docty ask js "copiar un objeto" --simple

# pregunta en español: recupera, traduce términos y sintetiza
docty ask js "cómo copio un objeto sin mutarlo"

# la forma abreviada también sintetiza consultas de varias palabras
docty js "diferencias entre let y const"

# solo recuperación, sin gastar cuota del modelo
docty ask js "qué hace structuredClone" --no-llm

# ver los fragmentos que recibió el modelo
docty ask js "copiar un objeto" --sources

# configuración sin editar el TOML a mano
docty config
docty config set llm.provider groq
docty config get llm.model
docty config unset llm.model

# caché de respuestas
docty cache
docty cache clear

# script-friendly
docty js Object.assign --json | jq '.results[0].summary'
docty ask js "copiar un objeto" --json | jq '.answer'
docty config show --json | jq '.values["llm.provider"]'
```

## Configurar el LLM

Docty habla el protocolo OpenAI-compatible. Hay presets para **Groq** y
**OpenRouter**, y cualquier endpoint compatible (por ejemplo, un servidor
local) se puede usar con `provider = "openai-compatible"`.

1. Crea una API key gratuita en [Groq](https://console.groq.com/keys) o
   [OpenRouter](https://openrouter.ai/keys).
2. Guarda la clave en el keyring del sistema y elige el proveedor:

```bash
docty auth login
```

El comando pide proveedor, clave y, para `openai-compatible`, la URL base y el
modelo. También acepta `--provider`, `--model`, `--base-url` y
`--api-key-stdin` para scripts. La clave **no** se escribe en el archivo de
configuración: se guarda en el keyring del sistema y se borra cualquier
`api_key` en texto plano que hubiera. La clave se comprueba contra el proveedor
salvo que se use `--no-validate`; si no hay red, se guarda igualmente con un
aviso.

```bash
# sin interacción
DOCTY_API_KEY="gsk_..." docty auth login --provider groq

# comprobar el proveedor y el origen de la clave
docty auth status --online

# borrar la clave del keyring
docty auth logout
```

Si el keyring no está disponible (por ejemplo, en un servidor sin D-Bus),
exporta la clave como variable de entorno:

```bash
export DOCTY_API_KEY="gsk_..."
```

La clave se resuelve en este orden: `DOCTY_API_KEY`, `llm.api_key` y, por
último, el keyring. `docty auth login` elimina `llm.api_key` para no dejar
secretos en texto plano.

Modelos por defecto de los presets:

| Proveedor | Modelo |
| --- | --- |
| `groq` | `openai/gpt-oss-20b` |
| `openrouter` | `google/gemma-4-31b-it:free` |

Puedes cambiarlo con `docty config set llm.model <modelo>` o volver al default
del proveedor con `docty config unset llm.model`. Si el proveedor responde que
el modelo no existe (HTTP 404), Docty lo indica y sugiere esos mismos comandos.

La configuración del proveedor queda en `~/.config/docty/config.toml`:

```toml
[llm]
provider = "groq"              # groq | openrouter | openai-compatible
model = "openai/gpt-oss-20b"   # opcional: hay default por proveedor
timeout = "20s"
max_tokens = 600
answer_lang = "auto"           # auto | es | en
```

Para OpenRouter:

```bash
docty auth login --provider openrouter
```

Para un endpoint propio:

```bash
docty auth login --provider openai-compatible \
  --base-url http://127.0.0.1:1234/v1 --model mi-modelo
```

```toml
[llm]
provider = "openai-compatible"
base_url = "http://127.0.0.1:1234/v1"
model = "mi-modelo"
```

En `openai-compatible`, la clave del keyring se guarda asociada a la URL base,
así que varios endpoints pueden usar claves distintas. Si tu endpoint local no
requiere clave, configura `provider`, `base_url` y `model` directamente en
`config.toml`; no hace falta `auth login`.

Si no hay proveedor o clave, Docty **no falla**: muestra los fragmentos
recuperados, avisa una sola vez y sale con código 0. Con `--require-llm` el
código es 4.

La respuesta se cachea en `~/.cache/docty/answers/` y se invalida si cambia la
pregunta, el modelo, el idioma de respuesta, los fragmentos usados o el
contenido de un fragmento.

### Gestionar la caché

```bash
docty cache          # ruta, respuestas, tamaño, rango y aviso del LLM
docty cache clear    # borra las respuestas y el aviso único
docty cache path
```

`cache clear` elimina solo el directorio `answers/` y el marcador `llm-hint`;
no toca otros archivos en la raíz del directorio de caché. Es idempotente. Con
`--json` devuelve `answers_removed`, `bytes_freed` y `hint_removed`.

## Configuración

`~/.config/docty/config.toml`:

```toml
default_lang = "js"
format = "auto"       # auto | tty | md | json
pager = "auto"        # auto | never
bootstrap = true

[llm]
provider = ""
model = ""
base_url = ""
api_key = ""          # legado; preferible docty auth login o DOCTY_API_KEY
timeout = "20s"
max_tokens = 600
temperature = 0
answer_lang = "auto"
```

Precedencia: flags > variables de entorno > `config.toml` > valores por defecto.

Variables: `DOCTY_LANG`, `DOCTY_CONFIG`, `DOCTY_DATA_DIR`, `DOCTY_CACHE_DIR`,
`DOCTY_LLM_PROVIDER`, `DOCTY_LLM_MODEL`, `DOCTY_LLM_BASE_URL`, `DOCTY_API_KEY`,
`DOCTY_NO_PAGER`, `NO_COLOR`.

### Gestionar la configuración

`docty config` muestra los valores efectivos y el origen de cada uno:
`default`, `config`, `env DOCTY_...`, `flag --lang` o `preset <proveedor>`.
La API key nunca se imprime; solo se muestra su origen (`env`, `config`,
`keyring` o `none`).

```bash
docty config                       # tabla efectiva + origen
docty config get llm.provider
docty config set llm.provider groq
docty config set llm.timeout 30s
docty config unset llm.model
docty config path
```

`config set` valida el valor antes de escribir, conserva comentarios y secciones
desconocidas y escribe el archivo con permisos `0600`. No permite
`llm.api_key`: para guardar una clave usa `docty auth login` o `DOCTY_API_KEY`.
Si una variable de entorno o `--lang` tiene prioridad sobre la clave que acabas
de escribir, el comando lo avisa. Al cambiar `llm.provider` no se borran
`base_url` ni `model` en silencio; si quedan valores heredados que no
corresponden, se muestra un aviso.

`config unset llm.api_key` sí elimina una clave en texto plano que hubiera
quedado de una configuración antigua. `--config RUTA` permite usar otro archivo;
`config set/unset` lo crean si no existe.

`docty config show --json` devuelve `config`, `exists` y `values`, con
`value`/`source` por clave. Para `llm.api_key` solo incluye `source` y `set`.
`docty config get <clave> --json` devuelve `key`, `value` y `source`; para la
API key omite `value`.

## Salida JSON

Lookup:

```json
{
  "schema_version": 1,
  "query": "Object.assign",
  "lang": "js",
  "mode": "lookup",
  "engine": "retrieval",
  "results": [{ "symbol": "Object.assign", "kind": "static-method", "...": "..." }],
  "timings_ms": { "retrieval": 1, "total": 1 }
}
```

Respuesta sintetizada:

```json
{
  "schema_version": 1,
  "query": "copiar un objeto",
  "lang": "js",
  "mode": "ask",
  "engine": "llm",
  "answer": "Usa spread o Object.assign [1]...",
  "grounded": true,
  "cached": false,
  "citations": [{ "n": 1, "symbol": "Object.assign", "url": "https://..." }],
  "results": [{ "symbol": "Object.assign", "sections": [{ "heading": "...", "body": "..." }] }],
  "timings_ms": { "retrieval": 0, "llm": 812, "total": 812 }
}
```

`grounded` es `false` si la respuesta no cita fuentes válidas; en ese caso
Docty avisa por stderr y sigue mostrando la respuesta.

Sin proveedor LLM, `docty ask --json` no cae al texto humano: devuelve el mismo
envelope con `mode: "ask"`, `engine: "retrieval"`, `results` y `warnings`.

`list --json` devuelve `{schema_version, lang, count, results}`;
`sources --json` devuelve `{schema_version, sources}`;
`ingest --json` devuelve `{schema_version, lang, count, documents}` y, con
`--dump`, `{schema_version, document}`. Si una página usa macros que Docty no
sabe expandir, el documento incluye `unknown_macros` y `ingest` avisa por
stderr. `doctor --json` devuelve un informe con
`config`, `index`, `snapshot`, `provider`, `cache` y `effective`.

Los errores previstos con `--json` también van a stdout:

```json
{
  "schema_version": 1,
  "error": {
    "code": "no_results",
    "message": "no encontré \"...\" en js",
    "exit_code": 1
  }
}
```

El mensaje humano se sigue escribiendo en stderr y el código de salida no cambia.

`docty auth status --json` usa el mismo `schema_version` y devuelve `provider`,
`model`, `base_url`, `key_source` (`env`, `config`, `keyring` o `none`) y la
ruta de `config`. `docty config show --json` y `docty cache --json` siguen el
mismo esquema versionado; `docty config get llm.api_key --json` nunca incluye
el valor de la clave.

## Códigos de salida

| Código | Significado |
|---|---|
| 0 | éxito (incluye fallback sin LLM) |
| 1 | sin resultados |
| 2 | uso incorrecto |
| 3 | fuente, índice, red o keyring no disponible |
| 4 | `--require-llm` y el LLM no está disponible |
| 130 | interrumpido (SIGINT) |

## Cómo funciona

1. **Ingesta**: se parsea el Markdown de MDN, se expanden macros (`jsxref`,
   `DOMxRef`, `Glossary`…) y se descartan las que solo existen en la web
   (`InteractiveExample`, `Specifications`, `Compat`). Las macros desconocidas
   se reportan en `unknown_macros` en vez de perderse en silencio.
2. **Chunking**: cada página se corta por headings `##`, `###` y `####`; los
   bloques de código nunca se parten.
3. **Índice**: SQLite con FTS5 y tokenizer Porter. El símbolo se deriva del slug
   y del `page-type` (`Array.prototype.map`, `Object.assign`,
   `Array.prototype[Symbol.iterator]`, `Spread syntax`).
4. **Consulta**: lookup exacto → alias de prototipo (`Array.map`) → sufijo
   (`map`) → desambiguación → búsqueda FTS → sugerencias por distancia de
   edición. Los snippets eliminan enlaces, fences y énfasis Markdown.
5. **Español**: un glosario ES→EN expande la consulta (`copiar` → `copy`,
   `clone`; `objeto` → `object`). Si la consulta estricta no devuelve nada, se
   relaja a OR. La detección de idioma combina acentos, palabras funcionales y
   términos del glosario; una palabra ambigua como `a` no basta.
6. **Síntesis**: se envían como máximo 6 fragmentos (12 000 caracteres) al
   modelo con instrucciones de citar `[n]` y no inventar APIs. El contexto
   conserva los bloques de código y convierte los enlaces relativos de MDN en
   absolutos. Las citas se validan contra los fragmentos; si no hay citas
   válidas, `grounded=false`.

## Desarrollo

```bash
make test
make vet
```

Los fixtures de `testdata/mdn` son páginas reales de MDN usadas por los tests.
La integración con el LLM se prueba con un servidor HTTP falso, sin red.

## Licencia y atribución

El código de Docty se distribuye bajo la licencia del repositorio. El contenido
de la documentación indexada proviene de MDN y se rige por
[CC BY-SA 2.5](https://creativecommons.org/licenses/by-sa/2.5/); cada resultado
incluye la URL de la fuente original.
