# 0001. Inference via inferd (local) and OpenAI-compatible (remote); drop embedded Ollama and Bedrock SDKs

- Status: accepted
- Date: 2026-07-12

## Context

RemoteClaw embedded two inference backends directly: the Ollama Go SDK
(`github.com/ollama/ollama`) for local models and the AWS Bedrock runtime SDK
(`github.com/aws/aws-sdk-go-v2/*`) for cloud models, selected by an `auto`
provider that keyed off the presence of `AWS_ACCESS_KEY_ID` /
`AWS_SECRET_ACCESS_KEY` in the environment.

This coupled RemoteClaw to specific engines and pulled a large dependency tree
(the full AWS SDK) into a security-sensitive remote-execution agent. It also
meant RemoteClaw itself needed cloud credentials in its environment to reach a
cloud model — credentials that then live next to a tool that can run arbitrary
commands.

Two facts reshaped the options:

1. `github.com/3rg0n/inferd` is a host-wide local inference daemon that owns the
   warm model, picks the compute backend, and exposes a small IPC surface over a
   Unix socket / Windows named pipe (no network listener). Its Go client
   supports tool calling, which the agentic loop requires.
2. inferd — and the broader ecosystem (Ollama's `/v1`, mantle/Bedrock-as-OpenAI,
   Anthropic's OpenAI-compatible API, vLLM, LM Studio, LocalAI) — already speak
   the OpenAI chat-completions wire format. A single OpenAI-compatible client
   covers every remote backend we care about, including Bedrock via a gateway.

## Decision

Replace the `auto`/`local`/`bedrock` scheme with two transports behind the
existing `ai.Converser` interface:

- **`inferd`** (local, default): the `github.com/3rg0n/inferd/clients/go` client
  over the platform-default socket (or an explicit `ai.inferd_socket` override).
  The daemon owns the model, so RemoteClaw carries no model/pull logic for this
  path.
- **`openai-compat`** (remote): the official `github.com/openai/openai-go`
  client pointed at any `ai.openai_base_url`. This subsumes both the former
  Ollama path (point at `http://localhost:11434/v1`) and the former Bedrock path
  (point at a mantle/OpenAI gateway).

Provider resolution: an explicit `ai.provider` wins; empty resolves to
`openai-compat` when `ai.openai_base_url` is set, otherwise `inferd`.

Remove `github.com/ollama/ollama` and all `github.com/aws/aws-sdk-go-v2/*`
dependencies, the `aws:` config block, `ai.ollama_host`, and the two-place
`phi4 → claude` model override.

The official OpenAI SDK was chosen over `sashabaranov/go-openai` because the
project's mock server (`../mock-apis`) is CI-conformance-validated by
`openai/openai-go` specifically, giving the highest-confidence local test target
when we use that same client.

## Consequences

- **Easier:** RemoteClaw no longer needs cloud credentials in its own
  environment — reaching a cloud model is the gateway/daemon's job. The
  dependency tree shrinks substantially (the whole AWS SDK is gone). Adding a
  new backend is a config change (a base URL), not new code. The `Converser`
  seam means the processor loop and its tests are unchanged.
- **Harder / new constraints:** local inference now depends on a *separately
  installed, running* inferd daemon rather than an in-process SDK. inferd runs
  as a per-user service (see the runtime-model note in the README), which must
  be reachable by RemoteClaw's service account — a deployment consideration that
  did not exist when inference was in-process. Temperature remains capped at 0.3
  for safety consistency regardless of backend.
- The `auto`-via-AWS-env-var behavior is gone; existing configs that relied on
  it must set `ai.provider` / `ai.openai_base_url` explicitly.
