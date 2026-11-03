# The agentic layer: setup and usage

The agent lets a language model read and write your PocketBase records
through typed tools, behind a permission gate that asks before every
change.

This guide is the **how**: how to turn it on, how to use it, how to
configure it, and how to take it out again.

- For the *how it works*, read
  [`book/ch22-agentic-layer.md`](../book/ch22-agentic-layer.md).
- For the *exact API*, read
  [`docs/agents/REFERENCE.md`](agents/REFERENCE.md).
- For *adding a tool*, read
  [`docs/agents/add-an-agent-tool.md`](agents/add-an-agent-tool.md).

---

## 1. The agent is optional

Nothing in the template depends on the agent. Without a provider the
application starts, the records page works, and the agent page says
that no model is configured:

```
 Agent

 Ask about your records. The agent reads and writes them through the tools.
```

A project that does not want a model at all can delete the layer
entirely — see [§8](#8-removing-the-agent).

---

## 2. Turning it on

Three things are needed: a **provider**, a **model**, and an **API
key**.

### The configuration file

```toml
# flint.toml
[agent]
provider = "openai"            # openai | openaicompat | openrouter | local
model = "gpt-4o"
api_key_env = "OPENAI_API_KEY" # the key is read from the environment, never from the file
permission_mode = "ask"        # ask before every write, or allow
max_steps = 12
```

### The environment

The API key itself is **never** read from the configuration file. A
file is easy to copy around; an environment variable is not. The
configuration names the variable to read:

```bash
export OPENAI_API_KEY="sk-..."
```

If `api_key_env` is omitted, the conventional variable of the provider
is used: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`,
`GOOGLE_API_KEY`, `AZURE_API_KEY` or `AWS_ACCESS_KEY_ID`.

Only `OPENAI_API_KEY` and `OPENROUTER_API_KEY` apply to the providers
the template builds — the others are there for the providers a
downstream project may add. `openaicompat`, `openai-compatible` and
`local` have no conventional name, so they need an explicit
`api_key_env`.

A missing key does not stop the application: the model is built
without it and the failure shows up as a rejected request. A local
server usually ignores the key, but it still has to be set to
something.

### The environment can replace the file

Every setting has an environment variable. They beat the configuration
file, and the flags beat both:

| Setting             | Environment variable            | Default   |
| ------------------- | ------------------------------- | --------- |
| `provider`          | `FLINT_AGENT_PROVIDER`          | none      |
| `model`             | `FLINT_AGENT_MODEL`             | none      |
| `base_url`          | `FLINT_AGENT_BASE_URL`          | none      |
| `api_key_env`       | `FLINT_AGENT_API_KEY_ENV`       | per provider |
| `system_prompt`     | `FLINT_AGENT_SYSTEM_PROMPT`     | built-in  |
| `permission_mode`   | `FLINT_AGENT_PERMISSION_MODE`   | `ask`     |
| `max_steps`         | `FLINT_AGENT_MAX_STEPS`         | `12`      |

```bash
export FLINT_AGENT_PROVIDER=openai
export FLINT_AGENT_MODEL=gpt-4o
export OPENAI_API_KEY="sk-..."
```

> **Never commit a token.** `flint.toml` names the *variable*, not the
> key. A `.env` file is not read — export the variable in your shell.

---

## 3. The providers

The template builds the **OpenAI compatible** providers only. They
cover OpenAI itself, OpenRouter and any local server, and they do not
pull the AWS and Google SDKs into the binary.

| `provider`      | What it is                                          | Needs `base_url`? |
| --------------- | --------------------------------------------------- | ----------------- |
| `openai`        | The OpenAI API                                       | no                |
| `openaicompat`  | Any OpenAI compatible API                            | yes               |
| `openai-compatible` | An alias of `openaicompat`                        | yes               |
| `openrouter`    | OpenRouter, through the compatible API                | no                |
| `local`         | A local server, such as Ollama or LM Studio           | yes               |

```toml
[agent]
provider = "local"
model = "llama3.1"
base_url = "http://localhost:11434/v1"
api_key_env = "OLLAMA_API_KEY"   # the local server usually ignores it
```

The `local` provider has no conventional variable name, so
`api_key_env` is required — set it to anything, and export it.

### Adding another provider

The switch is in one place — `newModel` in `cmd/tui/agent.go`:

```go
func newModel(cfg *config.Config, apiKey string) (fantasy.LanguageModel, error) {
	provider := strings.ToLower(cfg.Agent.Provider)

	switch provider {
	case "openai":
		// ...
	case "openaicompat", "openai-compatible", "openrouter", "local":
		// ...
	default:
		return nil, fmt.Errorf("cmd/tui: unknown agent provider %q", cfg.Agent.Provider)
	}
}
```

Add a case, build the model, and return it. `fantasy.LanguageModel` is
the only interface the runner needs.

---

## 4. Using it

Press `7` to open the agent page, type a question, and press `enter`.

```
 Agent

 you: Which records have no label?
 → list_records {"collection":"vocab","filter":"label = ''"}
 I found 4 of the 128 records.
```

- `enter` — send the prompt.
- `esc` — cancel a running turn, or clear the prompt.
- `ctrl+b` — open the side panel, on a terminal of at least 121
  columns. The panel shows the conversation next to the records.

### The permission gate

When the model wants to **change** something, the application asks
first:

```
 The agent wants to Create a new record in the collection.
 {"collection":"vocab","fields":{"key":"alpha"}}
 y allow · n deny
```

`y` runs the write; `n` refuses it and the model is told, so it can
explain or try another way. **A refusal is not a failure.**

`permission_mode = "allow"` skips the question. Only do this where
nobody is watching — a demo, a test, a sandbox. The default is `ask`,
and it should stay that way for anything that touches real data.

### The conversations

The turns are stored in PocketBase, in the `_flint_sessions` and
`_flint_messages` collections. A turn continues where the last one
stopped: the model sees the history of the conversation.

The collections are created on the first run, by the migration in
`internal/session/setup.go`. If they cannot be created the application
still runs — it just does not remember the conversations, and says so
in the log.

---

## 5. The tools

The model sees eight tools. They are the only way it touches the
data.

| Tool                | What it does                     | Gated? |
| ------------------- | -------------------------------- | ------ |
| `list_collections`  | Lists the collections             | no     |
| `list_records`      | Lists the records of a collection | no     |
| `get_record`        | Fetches one record                | no     |
| `search_records`    | Searches the records of a collection | no   |
| `count_records`     | Counts the records of a collection | no     |
| `create_record`     | Creates a record                  | **yes** |
| `update_record`     | Updates a record                  | **yes** |
| `delete_record`     | Deletes a record                  | **yes** |

The read tools are not gated: they cannot change anything. The write
tools are, because they change the data of the user.

Two bounds keep a runaway model from exhausting the process:

- A tool call is cut off after **30 seconds**.
- A listing tool returns at most **100 records**, and 30 by default.

### The arguments

Every tool takes a `collection` and a few more. The names matter:
they are the contract with the model.

| Tool             | Arguments                                                    |
| ---------------- | ------------------------------------------------------------ |
| `list_records`   | `collection`, `filter`, `sort`, `page`, `per_page`            |
| `get_record`     | `collection`, `id`                                           |
| `search_records` | `collection`, `term`, `per_page`                             |
| `count_records`  | `collection`, `filter`                                       |
| `create_record`  | `collection`, `fields`                                       |
| `update_record`  | `collection`, `id`, `fields`                                 |
| `delete_record`  | `collection`, `id`                                           |

`search_records` takes a **term**, not a filter: it matches the term
against the `title` field of the records. To filter on a field, use
`list_records` with a `filter`.

### The filters are the native grammar

The tools take the same filters as the pages — the native PocketBase
grammar, not SQL:

```
list_records {"collection":"vocab","filter":"status = \"active\"","sort":"-created"}
```

The native grammar cannot reach into a JSON array. The
`hybridstore.Hybrid` wrapper evaluates those expressions locally, so
`meta.tags.#(=="cli")` works too — but only when the store is wrapped
in it. See [`docs/agents/filter-json-fields.md`](agents/filter-json-fields.md).

---

## 6. The system prompt

The template ships a prompt that tells the model what it can do, and
that it must expect to be asked before it changes anything.

Override it with `system_prompt`:

```toml
[agent]
system_prompt = """
You are the assistant of a terminal application that reads and writes PocketBase records.

Answer only from the tools. When the data does not answer the question, say so.
"""
```

The default prompt is in `defaultAgentSystemPrompt` in
`cmd/tui/agent.go`.

---

## 7. The bounds

| Bound              | Default | Where              |
| ------------------ | ------- | ------------------ |
| Model calls a turn | 12      | `max_steps`        |
| A tool call       | 30 s    | `toolTimeout`      |
| A whole turn      | 5 min   | `DefaultRunTimeout` |
| Records per page  | 30 / 100 max | `clampPerPage`  |

`max_steps` is the important one: a model that keeps calling a tool
that returns the same result will never stop on its own.

---

## 8. Removing the agent

The layer is self-contained. To take it out:

1. Delete `internal/agent/` and `internal/session/`.
2. Delete `internal/pages/agent.go` and its test.
3. Delete `internal/components/agentpanel.go` and `internal/app/panel.go`
   and their test.
4. Remove the `agentPage` and `panel` fields from the `Model` in
   `internal/app/app.go`, the `case state.PageAgent:` in
   `updateActivePage`, the `ctrl+b` case in `handleGlobalKey`, and the
   `case state.PageAgent:` in the `View`.
5. Remove the `PageAgent` constant from `internal/state/state.go`, and
   the `7` case in `handleGlobalKey`.
6. Remove the `[agent]` section from the configuration, and the
   `AgentConfig` from `config/config.go`.
7. Remove `cmd/tui/agent.go`, and the `newRunner` call in `main.go`.

Then `make ci`.

---

## 9. Troubleshooting

**The agent page says "no agent is configured".**
The provider or the model is missing. Check `flint.toml` and the
`FLINT_AGENT_*` variables — the configuration file and the environment
are separate, and a value in one does not satisfy the other.

**"unknown agent provider".**
The provider is not one of the five the template builds. See
[§3](#3-the-providers).

**The agent answers without using the tools.**
The system prompt was replaced by one that does not ask for the tools.
Check `system_prompt`.

**Every write is refused.**
The gate has no requester, or nobody answered the question. The error
is `agent: no permission requester: create_record`. A gate that cannot
ask **refuses** — that is the design, not a bug.

**`permission_mode` does not seem to take effect.**
Only `allow` (or `yolo`) turns the questions off. **Any other value —
including a typo — falls back to `ask`**, which is the safe default.
Check the spelling in `flint.toml` and in `FLINT_AGENT_PERMISSION_MODE`.

**The turn stops after a few calls.**
`max_steps` was reached. Raise it, or give the model a prompt that
needs fewer calls.

**The conversations are not remembered.**
The `_flint_sessions` and `_flint_messages` collections could not be
created — usually a read-only data directory. The log says
`the conversations cannot be stored`.

---

## 10. See also

- [`book/ch22-agentic-layer.md`](../book/ch22-agentic-layer.md) — how
  the layer works, and why it is shaped that way.
- [`docs/agents/add-an-agent-tool.md`](agents/add-an-agent-tool.md) —
  adding a tool of your own.
- [`docs/agents/REFERENCE.md`](agents/REFERENCE.md) — the exact API.
- [`docs/GETTING_STARTED.md`](GETTING_STARTED.md) — the rest of the
  application.
