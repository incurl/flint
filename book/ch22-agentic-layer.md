# Chapter 22 — The Agentic Layer: When the Model Drives

> **Want to use it rather than build it?** The setup and usage guide is
> [`docs/AGENT_SETUP.md`](../docs/AGENT_SETUP.md): the providers, the
> settings, the tools, the troubleshooting, and how to take the layer
> out again. This chapter is about *how it works*.

## 22.1 The problem

So far the model has been a *reader*: the user types a filter, the
application runs a query, the screen shows records. The user drives.

The agentic layer inverts that. The user types a **question** — "which
of my vocabularies has no label?" — and a language model decides which
queries to run, in what order, and what to do with the answers. The
model *drives*.

That is a real change in the shape of the application, and it is worth
being precise about what it means before we build it:

| | Read-only agent | Agent that writes |
| - | - | - |
| The model calls | `list_records`, `count_records` | `create_record`, `delete_record` |
| Failure costs | a wrong answer | a wrong record |
| Needs | a tool registry | a tool registry **and a permission gate** |
| Needs | nothing | the conversation persisted |

The last row is the one people forget. A conversation that is not
persisted cannot be continued, cannot be reviewed, and cannot be
audited. In this template the conversations live in PocketBase, like
everything else.

## 22.2 The layer, in one picture

```
        the user types a question
                    │
                    ▼
        ┌───────────────────────┐
        │  internal/pages       │  the chat screen: prompt, stream,
        │  agent.go             │  tool calls, approvals
        └───────────┬───────────┘
                    │  tea.Cmd  (off the render loop)
                    ▼
        ┌───────────────────────┐
        │  internal/agent       │  the runner: the lifecycle of a
        │  run.go, tool.go      │  turn, the tool registry, the gate
        │  permission.go        │
        └───────────┬───────────┘
                    │
        ┌───────────┴───────────┐
        ▼                       ▼
┌──────────────────┐   ┌──────────────────┐
│  internal/session │   │  internal/pbstore│
│  the conversation │   │  the records     │
└──────────────────┘   └──────────────────┘
```

Three rules hold this together.

**The agent knows nothing about the UI.** It publishes *events*; the page
draws them. This is the same separation the rest of the template
follows, and it is what makes the agent testable without a terminal.

**The UI knows nothing about the model.** The page receives
`AgentEventMsg` and `AgentDoneMsg`. It never sees a prompt, a token or
a provider.

**The data layer knows nothing about either.** The tools are ordinary
`pbstore` operations. The agent layer is a *caller* of the store, not
a second way to reach the data.

## 22.3 The loop is not yours

The first thing to know is that **the agent loop is not something this
template implements**. It comes from [`charm.land/fantasy`][fantasy],
the same library Crush uses:

```go
agent := fantasy.NewAgent(model,
    fantasy.WithSystemPrompt(systemPrompt),
    fantasy.WithTools(tools...),
    fantasy.WithStopConditions(func(steps []fantasy.StepResult) bool {
        return len(steps) >= maxSteps
    }),
)

result, err := agent.Stream(ctx, fantasy.AgentStreamCall{
    Prompt:   prompt,
    Messages: history,
    OnTextDelta: func(id, delta string) error { /* ... */ return nil },
    OnToolCall:  func(call fantasy.ToolCallContent) error { /* ... */ return nil },
})
```

The model call, the tool dispatch, the iteration between them and the
schema generation are all the library's. What the template adds is the
part that is specific to Flint:

- the **tools** that speak to PocketBase;
- the **events** the UI consumes;
- the **permission gate** in front of the writes;
- the **conversation**, persisted in PocketBase.

That division is worth copying. Re-implementing the loop is a large
amount of code to own, and the library is already tested against every
provider.

## 22.4 The tool interface

A tool is a capability the model can invoke:

```go
type Tool interface {
    // Name returns the identifier of the tool.
    Name() string
    // Description returns the prose the model reads to decide when to
    // call the tool.
    Description() string
    // Run executes the tool.
    Run(ctx context.Context, input string) (string, error)
}
```

Three parts, and the middle one is the one that matters.

⚠️ **The description is the single most important part of the tool.** It
is the only thing that tells the model when to call it. A vague
description produces a model that calls the wrong tool, and the fix is
not in the code — it is in the prose.

```go
const listRecordsDescription = "List the records of a PocketBase collection. " +
    "Use it to see what is stored before answering a question about the data. " +
    "The filter and the sort use the PocketBase grammar: " +
    "`status = \"active\"` for a filter, `-created,name` for a sort."
```

Notice what it does: it says what the tool does, *when* to use it, and
what the arguments mean. It is written for the model, not for the user
of the template.

### Arguments are structs, and the schema is generated

The arguments of a tool are a Go struct:

```go
type ListRecordsParams struct {
    // Collection is the name of the collection to list the records of.
    Collection string `json:"collection" description:"The name of the collection to list the records of."`
    // Filter narrows the records, in the PocketBase filter grammar.
    Filter string `json:"filter,omitempty" description:"A PocketBase filter expression, for example: status = \"active\". Leave empty to list everything."`
    // Page is the page number, starting at 1.
    Page int `json:"page,omitempty" description:"The page number to fetch, starting at 1."`
}
```

The `json` tag gives the argument its name in the call, `omitempty`
makes it optional, and the `description` tag explains it. The JSON
schema the model receives is **generated from the struct**, so the
description of an argument lives in exactly one place.

⚠️ **Put the explanation in the tag, not in a comment.** The model
never sees the comment. A parameter struct whose comments are
excellent and whose tags are empty produces a model that guesses.

### The tools of the template

Eight tools, all backed by the store:

| Tool | What it does | Gated |
| - | - | - |
| `list_records` | Lists the records of a collection | no |
| `get_record` | Fetches a single record by id | no |
| `search_records` | Searches the records for a text | no |
| `count_records` | Counts the records of a collection | no |
| `list_collections` | Lists the available collections | no |
| `create_record` | Creates a record | **yes** |
| `update_record` | Updates a record | **yes** |
| `delete_record` | Deletes a record | **yes** |

The read-only tools come first in the registry, and the order is
deliberate: the tool list is a list the model reads.

### A tool validates before it writes

```go
func runCreateRecord(ctx context.Context, store pbstore.Store, params *CreateRecordParams) (string, error) {
    if params.Collection == "" {
        return "", fmt.Errorf("agent: the collection is required")
    }

    if len(params.Fields) == 0 {
        return "", fmt.Errorf("agent: the fields are required")
    }

    record, err := store.Create(ctx, params.Collection, params.Fields)
    if err != nil {
        return "", err
    }

    return "created: " + describeRecord(record), nil
}
```

A model that invents a field name gets an error it can correct, not a
record with a phantom field.

⚠️ **A tool failure is a result, not a crash.** The adapter turns an
error into an *error response*, which the model reads and can react
to. A panic takes the whole run down, and the user sees the
application disappear.

### A tool is bounded

The listing tools clamp what the model asks for:

```go
const (
    defaultToolPerPage = 30
    maxToolPerPage     = 100
)
```

A model that asks for a million records must not be able to exhaust the
memory of the process. This is the kind of limit that looks like
unnecessary caution until the first model asks for "all the records".

## 22.5 The runner: the lifecycle of a turn

```go
type Options struct {
    Model        fantasy.LanguageModel
    Registry     *Registry
    SystemPrompt string
    Publisher    Publisher
    History      *History
    MaxSteps     int
    Timeout      time.Duration
}
```

The runner owns the lifecycle of a run and nothing else. It is
responsible for four things the library does not do.

**It publishes events.** The UI subscribes:

```go
type Event struct {
    RunID     string
    Kind      EventKind
    Text      string
    ToolName  string
    ToolInput string
    Err       error
}
```

Every event carries the id of the run it belongs to. That is the guard
every other page in the template applies: a late delta of a cancelled
turn must not appear in the next one.

**It bounds the run.** Twice:

```go
const DefaultMaxSteps   = 12
const DefaultRunTimeout = 5 * time.Minute
```

The step bound is what stops a model that keeps calling a tool in a
loop. A model that calls `count_records`, gets the same answer, and
calls it again would otherwise never stop.

**It can be cancelled.** The cancellation reaches the model call *and*
the tools, because both run under the run's context:

```go
func (r *Runner) Cancel(runID string) bool
func (r *Runner) CancelAll()
func (r *Runner) Busy() bool
```

**It records the turn.** More on that in §22.8.

## 22.6 The permission gate

This is the part that separates a demo from something you would let a
user type into.

The three mutating tools go behind a gate:

```go
registry.Register(NewGuardedTool(
    NewCreateRecordTool(store),
    gate,
    "Create a new record in the collection.",
))
```

The third argument is what the **user** reads when they are asked to
approve, so it says what the call would do, in the user's terms.

The gate has two modes:

```go
const (
    // PermissionModeAsk asks the user before every mutating tool.
    PermissionModeAsk PermissionMode = iota
    // PermissionModeAllow approves every mutating tool.
    PermissionModeAllow
)
```

⚠️ **A gate that cannot ask refuses.** This is the single most
important line in the permission layer:

```go
if requester == nil {
    return fmt.Errorf("%w: %s", ErrNoPermissionRequester, request.ToolName)
}
```

An agent that writes to the data of the user must not do it by
default. A missing requester is an *error*, not a silent approval. The
alternative — treating "nobody to ask" as "yes" — is how an agent
deletes a collection while the user watches a spinner.

### The gate is not a warning

A refused call **never reaches the tool**, so no write happens. The
test that proves it:

```go
func TestGuardedToolRefusesToWrite(t *testing.T) {
    // ... a store with three records ...

    tool := agent.NewGuardedTool(agent.NewCreateRecordTool(store), gate, "Create a record.")

    _, err := tool.Run(ctx, `{"collection":"vocab","fields":{"key":"delta"}}`)
    if !errors.Is(err, agent.ErrPermissionDenied) {
        t.Fatalf("Run() error = %v, want ErrPermissionDenied", err)
    }

    count, _ := store.Count(ctx, "vocab", "")
    if count != 3 {
        t.Errorf("the collection holds %d records, want 3: the refused write happened", count)
    }
}
```

The count is the assertion that matters. Asserting only the error would
pass even if the write happened and then was rolled back.

## 22.7 Asking the user: the decision broker

A run blocks on the answer of the user. The answer arrives through the
message channel of the UI. Something has to connect the two, and that
something is the **decision broker**.

```
   the run                          the UI
     │                                │
     │ broker.RequestPermission()      │
     │   ├─ registers the request      │
     │   └─ publishes it  ────────────►│  the page shows the request
     │                                │
     │                                │  the user presses y
     │                                │
     │◄──── broker.Resolve(id, allow) ─┤
     │
     │ the tool runs
```

The broker owns the id of the request:

```go
request.ID = id
b.requester.PublishPermission(request)
```

⚠️ **The id has to have exactly one owner.** The first version of this
had the broker keying its pending map by `permission-1` while the
publisher returned `request-1`, and the answer never arrived: the run
waited until the timeout. The test that caught it was the one that
resolved the request by the id the *publisher* had used.

Whoever owns the id, the answer must reach the run that asked for it.
Putting the id **in the request** makes the disagreement impossible:
there is only one id, and it is in the value that travels.

### A request that is never answered

The wait watches the context:

```go
select {
case decision := <-answers:
    return decision, nil
case <-ctx.Done():
    return PermissionDeny, ctx.Err()
}
```

A run that is cancelled while the user is reading the request does not
leave a waiter behind forever.

## 22.8 The conversation

The turns of a conversation live in PocketBase, like every other piece
of data of the template.

Two collections:

| Collection | What it holds |
| - | - |
| `_flint_sessions` | The metadata of a conversation |
| `_flint_messages` | One turn each |

The turn is stored as **JSON**:

```go
type payload struct {
    Role       string `json:"role"`
    Turn       int    `json:"turn"`
    Text       string `json:"text,omitempty"`
    ToolName   string `json:"tool_name,omitempty"`
    ToolInput  string `json:"tool_input,omitempty"`
    ToolOutput string `json:"tool_output,omitempty"`
}
```

⚠️ **A turn is not a flat record.** It carries the text of the
assistant, the tool calls it made and the results it got. Keeping the
shape of a turn out of the collection schema means a new kind of turn
does not need a migration — and it means the two backends agree without
a schema negotiation.

### Reading it back

```go
func (h *History) Load(ctx context.Context) []fantasy.Message
```

The stored turns become the messages the model library expects. A tool
turn becomes two parts — the call and its result — because the model
has to see both to continue the conversation.

### The test that matters

```go
func TestRunnerContinuesTheConversation(t *testing.T) {
    // ... two runs in the same session ...

    // The second call carries the first turn.
    for _, message := range model.calls[1] {
        for _, part := range message.Content {
            if text, ok := part.(fantasy.TextPart); ok && strings.Contains(text.Text, "first") {
                found = true
            }
        }
    }

    if !found {
        t.Error("the second call does not carry the first turn of the conversation")
    }
}
```

A conversation that is stored but never *read* would pass every other
test in this chapter. This one does not.

### Both backends

The embedded backend creates the collections on demand:

```go
session.EnsureCollections(ctx, store)
```

The remote backend cannot create a collection — the collections live on
the server — so the package also carries the migration:

```go
session.Migrations()
```

A deployment that switches backends keeps its conversations.

## 22.9 The screen

The chat screen is an **ordinary page**. It follows the same rules as
the rest of the template:

```go
func (p *AgentPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        // ...
    case AgentEventMsg:
        p.applyEvent(msg.Event)
        return p, nil
    case AgentDoneMsg:
        return p, p.applyDone(msg)
    case AgentPermissionMsg:
        p.pending = &msg
        return p, nil
    case tea.KeyPressMsg:
        return p, p.handleKey(msg)
    }
    return p, nil
}
```

The run happens in a command, off the render loop, like every other
command in the template.

### The events reach the UI through the program

A run happens in its own goroutine. The only door into the update loop
from another goroutine is `Program.Send`:

```go
func (p *programPublisher) Publish(event agent.Event) {
    program.Send(pages.AgentEventMsg{Event: event})
}
```

This is the bridge that makes the streaming work, and it is worth
noticing that it needs no new mechanism: Bubble Tea already has a way
to send a message from outside, and the template uses it.

### The prompt is modal

While the editor has the focus, the global hotkey layer is suspended —
including `q`, which would otherwise quit the program mid-sentence.

⚠️ **The page cannot take the focus in `Init()`.** The root model
calls `Init()` once, for the home page, so a page that is reached later
would never get the focus. The window resize is what focuses it:

```go
case tea.WindowSizeMsg:
    p.width = msg.Width
    p.height = msg.Height
    p.resize()

    // The prompt takes the keyboard as soon as the page is shown.
    p.input.Focus()
    return p, nil
```

## 22.10 The side panel

The chat page is a *page*: it takes the whole screen. That is right for
composing a question and for answering a permission prompt, but it is
the wrong shape for *watching* the agent work.

A second view of the same conversation solves this: a narrow panel on
the right, next to the records.

```
 Records · vocab                  │ Agent
 filter: go                       │ Which records mention Go?
 id       key      label          │ → search_records
 rec1     go       Go             │ I found 3 records that mention
 rec2     rust     Rust           │ Go in their description.
```

The panel is **read-only**. The writing — the prompt, the permission
questions — stays on the page, where there is room for it. A prompt in
a 32-column panel would be unusable, and a permission prompt has to
show what is about to change.

### The panel is a view, not a second copy

The panel does not own a conversation. It renders the entries of the
agent page:

```go
func (p *AgentPage) PanelEntries() []components.AgentPanelEntry {
    out := make([]components.AgentPanelEntry, 0, len(p.entries))
    for _, entry := range p.entries {
        // ...
    }
    return out
}
```

Two copies of the conversation would drift. One copy, rendered twice,
cannot.

### A run outlives the page it was started from

The panel makes a problem visible that was already there. A run
happens in a goroutine, and it keeps going after the user navigates
away. But the agent events were only delivered to the page that was
displayed:

```go
switch m.runtime.Page {
case state.PageRecords:
    // ...
case state.PageAgent:
    updated, cmd := m.agentPage.Update(msg)  // only here
    // ...
}
```

So the user started a run, pressed `2` to go back to the records, and
the answer was lost. The fix is to deliver the events of the agent to
the agent page whatever page is displayed:

```go
if m.runtime.Page != state.PageAgent {
    switch msg.(type) {
    case pages.AgentEventMsg, pages.AgentDoneMsg, pages.AgentPermissionMsg:
        updated, cmd := m.agentPage.Update(msg)
        m.agentPage = updated.(*pages.AgentPage)
        cmds = append(cmds, cmd)
    }
}
```

⚠️ **The guard matters.** Without it, the event is applied twice when
the agent page *is* the page that is displayed — once by the block
above and once by the switch below — and every entry of the
conversation appears twice. The test that catches it counts the
occurrences of a word in the panel.

### The width is a hard constraint

The record table is 88 columns wide. The panel takes 32, and the gap
between them takes 1. So the panel needs a terminal of at least

```
88 + 32 + 1 = 121 columns
```

Below that the table would be clipped — and a table whose viewport is
narrower than its columns renders **empty**, with no error and no
warning. The panel is therefore refused on a narrow terminal, and the
hint is not shown either: a toggle that cannot work is worse than no
toggle.

The panel also has to tell the pages about the narrower width. The
root model forwards the resize with the width of the *page*:

```go
case tea.WindowSizeMsg:
    m.width = msg.Width
    m.height = msg.Height
    m.resize()

    // The pages get the width the panel does not use.
    if m.pageWidth != m.width {
        msg.Width = m.pageWidth
    }
```

Without this the table would render at 121 columns inside a 88-column
page, and the layout would wrap.

### Composing two columns

The composition is plain text: pad the body to the width of the page,
and put the panel next to it.

```go
left = theme.Truncate(left, bodyWidth)
left = padRight(left, bodyWidth+gap)
padded = append(padded, left+right)
```

Two details are easy to get wrong, and both of them show up as a
broken layout rather than as an error:

- **The gap belongs to neither column.** `SplitWidth` returns the two
  widths *without* the gap, so the body is padded to
  `bodyWidth + gap`, not to `bodyWidth`. Get this wrong and every
  line is one column short.
- **Every line of the panel has the same width.** Lip Gloss draws the
  border around a block and does not pad the *lines* of that block, so
  a short line leaves a ragged border. The lines are padded before
  they are rendered.

The test that catches both of them is a width check:

```go
for i, line := range lines[:len(lines)-1] {
    if got := lipgloss.Width(line); got != width {
        t.Errorf("line %d is %d columns wide on a %d column terminal", i, got, width)
    }
}
```

## 22.11 Configuring the model

```toml
[agent]
provider = "minimax"
model = "MiniMax-M3.1-Flash"
api_key_env = "MINIMAX_API_KEY"
permission_mode = "ask"
max_steps = 12
```

Or in the environment:

```bash
export FLINT_AGENT_PROVIDER=minimax
export FLINT_AGENT_MODEL=MiniMax-M3.1-Flash
export MINIMAX_API_KEY=...
```

⚠️ **The API key is never read from the configuration file.** The file
names an environment variable, and the key is read from it. A file is
easy to copy around, an untracked file is easy to forget; an
environment variable is neither.

### Which providers?

The template ships the **OpenAI compatible** providers:

```go
case "openai":
case "minimax", "minimax-cn", "minimax-china":
case "minimax-global", "minimax-intl", "minimax-io":
case "openaicompat", "openai-compatible", "openrouter", "local":
```

They cover OpenAI itself, MiniMax, OpenRouter, and any local server.

MiniMax gets **two** names, because the platform runs in two places:
the Chinese one (`api.minimax.cn`) and the international one
(`api.minimax.io`). They have separate accounts and separate keys, so
one name with a configurable host would hide a mistake that shows up
as an authentication failure.

⚠️ **The other providers are heavy.** The Anthropic and the OpenRouter
packages of the model library pull the AWS and the Google SDKs into the
binary: the first build of this template timed out at 280 seconds
downloading them. For a template that has to compile and download
quickly, that is a poor trade. A downstream project that needs another
provider adds it in one file.

### The guided setup

Without a provider, the agent page does not show a prompt that cannot
work. It shows a **guided setup**: provider, then model, then key.

```
 Set up the agent

 Which provider?

 ▸ MiniMax
    MiniMax, the Chinese platform. api.minimax.cn
   MiniMax (global)
    MiniMax, the international platform. api.minimax.io
   …
```

The order is the order of the questions: the provider decides which
models are offered, so it comes first.

The last step shows what to copy:

```
 export MINIMAX_API_KEY="…"

 And put this in flint.toml:

   [agent]
   provider = "minimax"
   model = "MiniMax-M3.1-Flash"
   api_key_env = "MINIMAX_API_KEY"
```

Two decisions are baked into it, and both are about the secret.

**The key is masked.** It is typed in a `textinput` with
`EchoPassword`, because the screen may be shared or recorded.

**The key is never written anywhere.** The setup produces the
configuration to copy and the variable to export, and it writes
neither. A tool that rewrites `flint.toml` behind your back, or that
puts a key in a database, is making a security decision on your
behalf — and the setup is exactly where the user should be the one
making it.

### The example file

The last step offers to write the file, and the offer is worth
taking: the file it writes is a **complete, commented** `flint.toml`,
not a dump of the settings.

```go
// The setup does not depend on the configuration package: the
// components are widgets, and the application wires them.
func (s *AgentSetup) SetWriter(path string, writer func(path string, settings AgentSetupSettings) error)
```

The application owns the configuration, so it owns the writing:

```go
m.agentPage.SetConfigWriter(config.WritePath(cfg.Path()), func(path string, settings components.AgentSetupSettings) error {
    return cfg.SaveAgent(agentSettings(settings), path)
})
```

Three things are easy to get wrong in a writer like this, and each of
them is a security or a correctness bug rather than a crash.

**The key must not be in the file.** The settings that cross the
boundary have no field for it:

```go
type AgentSetupSettings struct {
    Provider  string
    Model     string
    BaseURL   string
    APIKeyEnv string  // the NAME of the variable, not the key
}
```

**The file must be private.** A configuration file may hold a remote
token, so it is written with `0600` — and atomically, through a
temporary file and a rename, so that a failure in the middle of the
write leaves the previous configuration intact.

**The values written must be the ones the file held.** The effective
configuration holds the environment variables and the flags, and those
do not belong in a file:

```go
// The values of the file are kept, before the environment and the
// flags override them.
snapshot := *cfg
cfg.fromFile = &snapshot
```

A setting that came from `FLINT_AGENT_PROVIDER` does not belong in
`flint.toml`, and writing it there would make the file disagree with
the way the application was actually started.

### The agent is optional

Without a provider, the application still starts, and the agent page
offers the guided setup. A template that cannot be run without an API
key is not a template.

## 22.12 Testing an agentic application

The agent layer is the part of the template that is easiest to test
badly, because it is the part that talks to something you do not
control.

The answer is the same as everywhere else in this book: **push the
model behind an interface**.

```go
type fakeModel struct {
    turns [][]fantasy.StreamPart
    calls int
}

func (m *fakeModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
    turn := m.turns[m.calls]
    m.calls++
    return func(yield func(fantasy.StreamPart) bool) {
        for _, part := range turn {
            if !yield(part) {
                return
            }
        }
    }, nil
}
```

With that, a test drives a whole turn — a tool call, the result, the
answer — without a network:

```go
model := &fakeModel{turns: [][]fantasy.StreamPart{
    toolTurn("count_records", `{"collection":"vocab"}`),
    textTurn("there are 42 records"),
}}

runner := agent.NewRunner(agent.Options{Model: model, Registry: registry})

text, err := runner.Run(ctx, agent.RunRequest{RunID: "r1", Prompt: "how many?"})
```

And the test that proves the loop iterates:

```go
if model.calls != 2 {
    t.Errorf("the model was called %d times, want 2", model.calls)
}
```

⚠️ **Test the tool, not the model.** The tools are ordinary store calls
against a real embedded PocketBase, and that is where the behaviour
lives. The model is a detail: it decides *when* to call the tool, not
*what the tool does*.

## 22.13 What it costs

The agentic layer is not free, and the costs are worth stating plainly.

**A dependency.** `charm.land/fantasy` and the provider SDKs. This is
the price of not writing the loop yourself.

**A second write path.** The user can now change the data through the
TUI *and* through the model. Every invariant that holds for the direct
path has to hold for the agent path too: the stale-response guard, the
permission gate, the store's own validation.

**A new failure mode.** The model can be wrong in a way that no amount
of testing catches, because the wrongness is in the choice of tool, not
in the code. This is why the gate exists and why the read-only tools
come first.

**Latency.** A turn is a network round trip per model call, and a turn
with tools is several. The UI has to show that it is working, which is
what the spinner is for.

## 22.14 Exercises

1. Add a tool that exports the records of a collection as CSV. Give it
   a schema, a description and a test.

2. Add a `permission_mode = "allow"` configuration path and check that
   the gate really stops asking. Verify that a run with no requester
   still refuses.

3. Write the missing test: a run that is cancelled *while* a
   permission request is pending. Assert that the waiter ends and that
   no write happened.

4. Add a session picker to the agent page: list the conversations of
   the store and let the user continue one. `session.Service.Sessions`
   and `Messages` are already there.

5. Add a tool that asks the *user* a question — the model asks, the
   page shows it, the answer goes back. The decision broker is the
   shape of it; the difference is that the answer is text, not a
   decision.

6. Give the agent a budget: count the tokens of a turn and refuse to
   start a turn that would exceed it.

7. Write a mock provider server and assert that the tool schema the
   model receives is the one the parameter structs describe.

## 22.15 What you learned

- The **loop is not yours**: it comes from `charm.land/fantasy`, and
  what the template adds is the tools, the events, the gate and the
  conversation.
- The **description of a tool** is the most important part of it: it
  is the only thing that tells the model when to call it.
- The **schema is generated from the parameter struct**, so the
  description of an argument lives in exactly one place — the tag.
- A tool **failure is a result**, not a crash: the model reads the
  error and corrects itself.
- A tool is **bounded**: the model cannot ask for a million records.
- The runner **publishes events** rather than drawing, and every event
  names the run it belongs to.
- A run is bounded **twice** — by steps and by time — and can be
  cancelled.
- The gate **refuses when it cannot ask**, and a refused call never
  reaches the tool.
- The **decision broker** is the meeting point between a run that
  blocks and a UI that waits for a key, and the id of the request has
  exactly one owner.
- The conversation lives in **PocketBase**, as JSON, so it travels with
  the rest of the data and needs no schema change for a new turn.
- The agent is **optional**: without a provider, the application still
  runs.

## 22.16 Further reading

- `internal/agent/run.go` — the runner and the events.
- `internal/agent/tool.go` — the tool interface and the registry.
- `internal/agent/tools.go` — the eight store tools.
- `internal/agent/permission.go` — the gate, the broker and the resolver.
- `internal/agent/history.go` — the bridge to the stored conversations.
- `internal/session/session.go` — the conversation storage.
- `internal/pages/agent.go` — the chat screen.
- `cmd/tui/agent.go` — the provider configuration.
- `docs/agents/add-an-agent-tool.md` — the recipe for adding a tool.
- `docs/AGENT_SETUP.md` — the setup and usage guide: the providers, the
  settings, the tools, the troubleshooting.

Next: [Chapter 23 — Extension Points: A Catalogue](ch23-extension-points.md).
