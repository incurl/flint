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
entirely — see [§9](#9-removing-the-agent).

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
cover OpenAI itself, MiniMax, OpenRouter and any local server, and
they do not pull the AWS and Google SDKs into the binary.

| `provider`      | What it is                                          | Needs `base_url`? |
| --------------- | --------------------------------------------------- | ----------------- |
| `minimax`       | MiniMax, the **international** platform              | no                |
| `minimax-cn`    | MiniMax, the **Chinese** platform                    | no                |
| `openai`        | The OpenAI API                                       | no                |
| `openaicompat`  | Any OpenAI compatible API                            | yes               |
| `openai-compatible` | An alias of `openaicompat`                        | yes               |
| `openrouter`    | OpenRouter, through the compatible API                | no                |
| `local`         | A local server, such as Ollama or LM Studio           | yes               |

### MiniMax

MiniMax is the provider the template documents first, and it is the
one the guided setup offers first. The two platforms have **separate
accounts** and **separate keys**:

| Platform         | `provider`   | Endpoint                      |
| ---------------- | ------------ | ----------------------------- |
| International    | `minimax`    | `https://api.minimax.io/v1`   |
| China            | `minimax-cn` | `https://api.minimax.cn/v1`   |

Both read the key from `MINIMAX_API_KEY`.

⚠️ **`minimax` is the international platform.** The two platforms have
separate accounts, separate keys and separate balances, and a key from
one is refused by the other — with an error that says the **balance is
insufficient**, not that the key is wrong. That error is misleading:
the account may be full of money on the other platform.

The endpoint in effect is on the settings page, under `agent.base_url`,
and in the log at startup.

The current models are `MiniMax-M3.1-Flash` and `MiniMax-M3`. The
first one is the default: it is the model the platform recommends for
agents and tool use, and it has a 1M token context window.

Both platforms speak the OpenAI chat protocol at `/v1/chat/completions`
and authenticate with `Authorization: Bearer <key>`, which is what the
template sends.

⚠️ **A provider that needs a key is refused one that has none.** The
application refuses to build a model without a key, rather than making
a request that carries no `Authorization` header at all — the
provider's answer to that says nothing about the variable that holds
the key, and sends the user looking for a header that was never there.

**Copy and paste this into `flint.toml`:**

```toml
# MiniMax, the international platform.
[agent]
provider = "minimax"
model = "MiniMax-M3.1-Flash"
api_key_env = "MINIMAX_API_KEY"
base_url = "https://api.minimax.io/v1"
permission_mode = "ask"
```

**And this into your shell:**

```bash
export MINIMAX_API_KEY="your-minimax-key"
```

**The international platform** is the same, with two lines changed:

```toml
# MiniMax, the Chinese platform.
[agent]
provider = "minimax-cn"
model = "MiniMax-M3.1-Flash"
api_key_env = "MINIMAX_API_KEY"
base_url = "https://api.minimax.cn/v1"
permission_mode = "ask"
```

The `base_url` line is optional: the template already knows the
endpoint of each platform. It is in the snippet so that the
configuration is complete and obvious — a reader does not have to
know which endpoint goes with which provider.

⚠️ **MiniMax does not serve a models endpoint through the compatible
API.** The model names are listed in `cmd/tui/agent.go` and in the
guided setup; a model that is not there will be refused by the
platform.

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
- `tab` — switch between the **plan** mode and the **act** mode.
- `esc` — cancel a running turn, or clear the prompt.
- `ctrl+b` — open the side panel, on a terminal of at least 121
  columns. The panel shows the conversation next to the records. The
  key is a global hotkey, so it does not work on this page: the prompt
  has the focus, and the global layer is suspended. The panel is
  closed when you come back to this page, and the status bar does not
  advertise the key here.

### The plan mode

`tab` switches the agent between two modes. In the **act** mode it
reads and writes the records, and the permission gate asks before
every change. In the **plan** mode it reads the records and proposes
what it would do, and it **cannot change anything**:

```
 Agent

 plan mode — the agent reads the records and proposes a plan, it does not change them.
```

Ask it what you want, and it produces a plan:

```
 you: which records are missing a label?
 → search_records {"collection":"vocab","filter":"label = ''"}
 # Plan

 1. Read the 4 records with no label.
 2. Look up the label of their key in the reference collection.
 3. Propose the label for each one.

 The plan is ready.
 enter implement it · esc dismiss it
```

Read it, then press `enter` to have it carried out, or `esc` to
dismiss it.

⚠️ **The restriction is structural, not a request.** In the plan mode
the tools that change the data are **not in the tool list at all** —
the model does not see them, so it cannot call them. A prompt that
said "do not change anything" would be a request, and a request can
be ignored; a tool that is not registered cannot be called at all.

That is why the mode is implemented in the registry and not in the
prompt: see § *The tools* below, and `internal/agent/mode.go`.

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

#### The compaction

A conversation that grows too long does not fit in the context window
of the model, and the run fails. So the conversation is **summarised**
when it passes 24 000 characters, before it is read:

- The older turns are replaced by a summary the model writes itself.
- The last six turns are kept as they are: they are the ones the
  conversation is about, and a summary of the last exchange loses the
  details the model is about to act on.
- The turns themselves are **not deleted**. They stay in the store —
  they are the history of the conversation — and the summary says how
  many of them it replaces.

The summary is a message of the conversation like any other, so it
survives a restart. The next turn reads it instead of the turns it
summarises.

⚠️ **The summary is written for the model, not for the user.** It
keeps the names of the collections, the fields and the identifiers
exact, because the assistant uses them to work with the data. A
summary that lost a field name is a summary the model cannot act on.

A conversation that cannot be summarised is not fatal: the run
continues with the whole history, which is what happened before.

### The guided setup

When the application starts **without a model**, the agent page shows
a guided setup instead of a prompt that cannot work. It walks through
the three things a setup needs:

```
 Set up the agent

 Which provider?

 ▸ MiniMax
    MiniMax, the Chinese platform. api.minimax.cn
   MiniMax (global)
    MiniMax, the international platform. api.minimax.io
   OpenAI
    The OpenAI API. Needs an OpenAI account.
   …

 ↑↓ choose · enter next · esc skip
```

The steps are **provider → model → key**. The provider decides which
models are offered, so the order matters.

The last step shows what to copy:

```
 Set up the agent

 The key is read from the environment. Export it:

 export MINIMAX_API_KEY="…"

 And put this in flint.toml:

   [agent]
   provider = "minimax"
   model = "MiniMax-M3.1-Flash"
   api_key_env = "MINIMAX_API_KEY"
   base_url = "https://api.minimax.cn/v1"

 Use the agent now, without a restart:

 a use it now
```

**The key is masked on the screen and it is never written to a file.**
The setup produces the configuration to copy and the variable to
export, and leaves the two to you — a secret belongs in the
environment, not in a file that is copied around and committed by
accident.

### Using it right away

Pressing `a` installs the model **in this session**, without a
restart:

```
 The agent is ready.
 The model is installed: it works from now on, without a restart.
```

The key you typed is used to build the model, and it is held in memory
only. The setup disappears and the chat takes its place — you can ask
a question straight away.

The model takes effect on the **next turn**: a request that is already
in flight keeps the model it started with.

⚠️ **The key is still needed in the environment for the *next* start.**
The model works now because the setup handed the key to the running
process; a new process finds it in the variable the configuration
names. So export it too, or the next start shows the setup again.

The setup is a *guide*, not a configuration writer: it does not
rewrite `flint.toml` behind your back, and it does not change the
environment of the process it runs in.

#### Writing the example file

The last step offers to write the file for you:

```
 Or write an example /home/you/project/flint.toml with every setting, commented:

 w write the file · esc back
```

Pressing `w` writes a **complete, commented** `flint.toml` — every
setting of the application, with the agent section filled in with what
you chose:

```toml
# flint.toml — the configuration of the application.
#
# Every setting is shown with the value it holds. Delete the lines
# you do not need: the default applies to the ones that are
# missing.
#
# The precedence is: flags > environment variables > this file.

# The store: "embedded" runs a local PocketBase, "remote" uses a
# server over REST.
mode = "embedded"
…
[agent]
# The provider: "minimax", "minimax-global", "openai",
# "openaicompat", "openrouter" or "local".
provider = "minimax"

# The model. The providers do not expose a models endpoint, so the
# name is the one the platform documents.
model = "MiniMax-M3.1-Flash"

# SECURITY: the API key is NEVER read from this file. This is the
# name of the environment variable that holds it.
api_key_env = "MINIMAX_API_KEY"
```

Three things are worth knowing about the file:

- **It is an example, not a dump.** Every setting is there with a
  comment that says what it does, so it doubles as the reference of the
  configuration. Delete the lines you do not need: the default applies
  to the ones that are missing.
- **The key is not in it.** Only `api_key_env` — the *name* of the
  variable that holds the key. The key itself never reaches the file.
- **It is written with `0600`**, because a configuration file may hold
  a remote token, and **atomically**: a failure in the middle of the
  write leaves the previous file intact.

The file is written where the application reads its configuration
from: the `--config` path if there is one, the existing `flint.toml`,
the XDG directory, and `flint.toml` in the working directory as the
last resort.

Writing the file is **not** what makes the agent work: pressing `a`
does that, and it works in this session. The file is for the *next*
one — which is why the key has to be in the environment too.

⚠️ **The values written are the ones the *file* held**, not the
effective ones. A setting that came from an environment variable or a
flag is not written into the file: it does not belong there.

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
| `todos`             | Manages the task list of the turn | no     |
| `create_record`     | Creates a record                  | **yes** |
| `update_record`     | Updates a record                  | **yes** |
| `delete_record`     | Deletes a record                  | **yes** |

The read tools are not gated: they cannot change anything. The write
tools are, because they change the data of the user.

**The plan mode takes the three write tools out of the list
altogether.** They are not gated in that mode — they are absent, so
the model cannot call them:

```go
// internal/agent/tool.go
func (r *Registry) AgentToolsFor(mode Mode) []fantasy.AgentTool {
	// ...
	for _, tool := range tools {
		if mode.IsPlan() && r.withheldInPlan(tool.Name()) {
			continue
		}
		// ...
	}
}
```

⚠️ **A tool the template does not know is withheld too.** A
downstream project that adds a tool is safe by default: it is kept
out of the plan mode until it is classified as read-only. The
classification lives in `readOnlyTools` in `internal/agent/mode.go`.

Two bounds keep a runaway model from exhausting the process:

- A tool call is cut off after **30 seconds**.
- A listing tool returns at most **100 records**, and 30 by default.

### The task list

The `todos` tool gives the model a structured list of the steps of a
turn. The page shows it above the prompt:

```
 To-do 1/3
 ✓ Find the records with no label
 → Proposing a label
 · Write the summary
```

Each task has a status — `pending`, `in_progress` or `completed` — and
an *active form*, the same task in the present continuous. The active
form is what is shown while the task runs: "Proposing a label" reads
better than "Propose a label" next to a spinner.

The list belongs to the conversation and lives **in memory**: it
organises a turn, it is not something to keep between runs. A task
without a status is `pending`, and a task without an active form gets
one derived from its content.

The tool is not gated: it changes nothing of the data, it changes what
the model is doing, and the user sees it. It is available in the plan
mode too — a plan is a list of steps.

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

### The loop detection

`max_steps` is a **budget**: twelve model calls, whatever they are.
It is blunt. Twelve calls that make progress is a normal turn; twelve
calls that are literally the same is a minute of the user's money
burned for nothing.

So there is a second bound, and it is a **diagnosis**. The agent
hashes each step — the tool call, its arguments and its result — and
when the same hash appears more than five times in the last ten steps,
the run stops:

| Bound                | Default | Where                    |
| -------------------- | ------- | ------------------------ |
| The same call repeats | 5 in 10 steps | `loopMaxRepeats` |

A model that calls the same tool with the same arguments and gets the
same answer is stuck. A model that calls it with *different*
arguments, or that gets a *different* answer, is making progress — and
the detection leaves it alone.

The run ends with `ErrLoopDetected`, and the page says so. The answer
the model produced is kept: the user wants to know what happened, not
just that something went wrong.

---

## 8. Persisting the configuration to PocketBase

**Short answer: the non-secret settings are fine; the API key is not.**

The preferences of the template are already persisted to PocketBase —
the theme, the scale, the page size and the compact mode live in the
`_flint_preferences` collection, and they are restored at startup. The
question is whether the *agent* configuration belongs there too.

### What can be persisted

| Setting | Persist? | Why |
| ------- | -------- | --- |
| `provider` | yes | Not a secret. It is a name. |
| `model` | yes | Not a secret. It is a name. |
| `base_url` | yes | Not a secret. It is a URL. |
| `permission_mode` | yes | Not a secret. It is a mode. |
| `max_steps` | yes | Not a secret. It is a number. |
| `system_prompt` | yes | Not a secret. It is an instruction. |
| `api_key_env` | yes | Not a secret. It is the *name* of a variable. |
| **the API key** | **no** | It is the secret itself. |

The first seven are exactly what the guided setup produces, so they
can be stored and restored without any new risk. The last one is the
problem.

### Why the key does not belong in the database

**1. The database is not a secret store.** A record in a PocketBase
collection is readable by anyone who can read the database file. In
the embedded mode that is `pb_data/data.db` — a SQLite file that is
copied, backed up and synced like any other data. PocketBase's
`_superusers` and the API tokens are protected by the framework, but a
record in a *base collection* is protected only by the API rules.

**2. The rules do not help on a local database.** The embedded store
runs with the full privileges of the process. A collection rule like
"only the owner may read" protects the *API*, not the file. Anyone
who can open the SQLite file can read the key.

**3. The remote backend cannot hold it either.** `Remote.Create` can
write, but a remote server needs a collection that the *server* owns,
with rules that the current token satisfies. The template's
`EnsureDictionaryCollection` is refused on the remote backend by
design, and the preferences follow the same rule: `SavePreferences`
returns `ErrPreferencesUnavailable` in remote mode. An agent key has
the same shape of problem and the same answer.

**4. There is already a mechanism for it.** The template reads the key
from an **environment variable that the configuration names**
(`api_key_env`). That is the standard mechanism, it is the one every
deployment tool uses, and it keeps the secret out of every artifact
the application produces: the database, the configuration file, the
logs, and the backups.

### The shape a working implementation would take

If you want the *non-secret* settings persisted — which is a good
idea, and it is what the guided setup produces — the implementation is
small and it fits the existing pattern:

```go
// internal/pbstore/preferences.go
type AgentPreferences struct {
    Provider        string
    Model           string
    BaseURL         string
    APIKeyEnv       string
    PermissionMode  string
    MaxSteps        int
    SystemPrompt    string
}

// The key itself is not here. There is no field for it, and there
// must not be one.
```

and a `_flint_agent_settings` collection with the same shape as
`_flint_preferences`:

```go
func LoadAgentPreferences(ctx context.Context, store Store) (AgentPreferences, error)
func SaveAgentPreferences(ctx context.Context, store Store, prefs AgentPreferences) error
```

`SaveAgentPreferences` refuses the remote backend, exactly like
`SavePreferences` does.

The precedence becomes:

```
flags  >  environment  >  the stored preferences  >  flint.toml
```

so a user who wants to override the stored setting for one run still
can, with `FLINT_AGENT_PROVIDER` or the flag.

### What we recommend

**Persist the settings, not the key.** It gives the convenience of the
guided setup — the provider and the model survive a restart — without
putting a credential in a file that gets copied around.

If you truly need the key stored, the honest options are:

- **A local secret file** with `0600` permissions, outside the data
  directory. Crush does this: it writes the key in plaintext to
  `~/.crush/crush.json` with `0600`. It works, and it is not much
  better than the environment variable.
- **The PocketBase settings**, encrypted with
  `embedded.encryption_env`. The embedded store already takes an
  encryption key. ⚠️ But that encrypts the PocketBase *settings*, not
  your records: a record in a collection is still plaintext in the
  SQLite file. It does not solve this problem.
- **The OS keychain** — `keyring`, `pass`, `secret-tool`. This is the
  right answer on a desktop, and it does not fit a template that has to
  run on a server and in a container.

### A note on the guided setup

The guided setup **writes the configuration file, and it uses the key —
but it never writes the key**.

Writing the file is not a security decision: a configuration file is a
document the user reads and edits, and the setup writes an *example*
of it — every setting, commented, with the agent section filled in.
The user still decides what to keep.

Using the key is not a security decision either: the key stays in the
process that the user typed it into, and it is used to build a model.
That is what makes the agent work without a restart.

Writing the key **would** be. A key in a file is a key in a file that
gets copied, backed up and committed by accident, and the setup has
no business making that choice for the user. So the key stays in the
environment variable the configuration names, and the file says which
one.

## 9. Removing the agent

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

## 10. Troubleshooting

**The agent page says "no agent is configured".**
The provider or the model is missing. Check `flint.toml` and the
`FLINT_AGENT_*` variables — the configuration file and the environment
are separate, and a value in one does not satisfy the other.

**"unknown agent provider".**
The provider is not one the template builds. See
[§3](#3-the-providers).

**"Please carry the API secret key in the 'Authorization' field".**
The provider refused the key. ⚠️ **The message is misleading**: it is
the *same* message for a key that is empty, one that is invalid, one
that belongs to the other platform, and one that carries a stray
space. It names the header, and the header is not the problem.

Run the diagnostic — it tells the four apart:

```bash
flint --check-agent
```

```
The agent configuration:

  provider    minimax
  model       MiniMax-M3.1-Flash
  base_url    https://api.minimax.io/v1
  api_key_env MINIMAX_API_KEY

The key:

  variable    MINIMAX_API_KEY
  exported    true
  length      42
  trimmed     false

Testing the key against https://api.minimax.io/v1 ...

The key works.
```

It reports the endpoint in effect — the one the application resolved,
even when the configuration leaves it empty — and it says whether the
variable is exported, whether it is empty, and whether the key had
whitespace around it.

⚠️ **A key exported in another shell is not visible.** The variable has
to be exported in the shell that starts the application, or in its
profile. A `.env` file is not read.

**The key is trimmed before it is sent.** A pasted key carries a
trailing space, and a trailing space makes the `Authorization` header
wrong — which the provider answers with the message above. The
diagnostic says so:

```
  trimmed     true

The key had whitespace around it. It is trimmed before it is sent.
```

A key that already carries the `Bearer ` prefix is accepted too: the
client adds the prefix itself, so it would otherwise be sent as
`Bearer Bearer ...`.

**MiniMax: "insufficient balance" or "invalid api key".**
The two platforms have **separate accounts, separate keys and separate
balances**. A key from `platform.minimax.io` does not work against
`api.minimax.cn`, and the other way round — and the error says the
balance is insufficient, which is misleading when the account is full
of money on the *other* platform.

Check that `provider` and the key match: `minimax` for
`api.minimax.io`, `minimax-cn` for `api.minimax.cn`. The endpoint in
effect is on the settings page, under `agent.base_url`.

**MiniMax: the model is refused.**
MiniMax does not serve a models endpoint through the compatible API, so
the template cannot discover the names. Use one of the current models:
`MiniMax-M3.1-Flash` or `MiniMax-M3`. The older `MiniMax-M2.x` and
`MiniMax-Text-01` models have been retired.

**MiniMax: the thinking is not shown.**
MiniMax returns the reasoning separately, in `reasoning_content`, and
the final answer in `content`. The template shows the final answer —
the reasoning is not rendered. Nothing is lost: the answer is complete.

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

## 11. See also

- [`book/ch22-agentic-layer.md`](../book/ch22-agentic-layer.md) — how
  the layer works, and why it is shaped that way.
- [`docs/agents/add-an-agent-tool.md`](agents/add-an-agent-tool.md) —
  adding a tool of your own.
- [`docs/agents/REFERENCE.md`](agents/REFERENCE.md) — the exact API.
- [`docs/GETTING_STARTED.md`](GETTING_STARTED.md) — the rest of the
  application.
