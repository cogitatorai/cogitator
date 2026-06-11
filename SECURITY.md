# Security Policy

## Reporting a vulnerability

Please report security issues privately. Do not open a public issue for a
vulnerability.

- **Preferred:** GitHub Security Advisories (private vulnerability reporting) on
  this repository. Use the "Report a vulnerability" button under the Security tab.
- **Fallback:** email security@cogitator.me.

What to expect:

- Triage within 72 hours of receipt.
- For confirmed issues, a fix or mitigation plan communicated within 14 days.

There is no bug bounty program.

## Supported versions

Cogitator is pre-1.0. Only the latest minor release receives security fixes
(currently the v0.46.x line). Older versions are not patched; upgrade to the
latest release.

## Threat model

Cogitator runs an LLM agent that chooses and executes tool calls (shell, file
I/O, browser control, HTTP fetch, MCP servers). Understand the following before
exposing an instance.

**The agent runs with the privileges of the server process.** Tool calls chosen
by the model execute under the daemon's user and permissions. The security layer
(sensitive-path blacklist, dangerous-command blacklist, domain allowlist) is
best-effort mitigation, not a sandbox; a determined or manipulated model can
still cause harm within those privileges. For any instance reachable beyond
localhost, run with Docker sandbox mode (`COGITATOR_SECURITY_SANDBOX=docker`),
which executes shell commands in throwaway containers with resource limits, a
read-only rootfs, and no network by default.

**Prompt injection is an inherent risk.** Content from tool results (web pages,
MCP servers, connectors) enters the model's context and can attempt to redirect
the agent. Retrieved memories are wrapped in boundary markers and the model is
instructed to treat them as data, not as directives to follow. This is
mitigation, not prevention; treat any agent with access to untrusted content as
capable of being steered by that content.

**Single owner or small trusted group.** Cogitator is designed for one person or
a small trusted group (a family or team). It is NOT hardened for hosting
untrusted users. Anyone you grant an account is inside the trust boundary.

**What is enforced server-side.** These controls are real and tested:

- JWT authentication on all non-public routes.
- Role checks on admin-only operations.
- Per-user ownership of sessions, memories, and tasks.
- Private-memory filtering applied at the SQL layer, so private memories are not
  returned across users.
- Secrets (API keys, bot tokens, OAuth credentials) stored in the OS keychain,
  with a file-based fallback written with `0600` permissions.

## Known limitations

Tracked hardening work is labeled in the public issue tracker. See open issues
with the `security` label:

https://github.com/cogitatorai/cogitator/issues?q=is%3Aissue+is%3Aopen+label%3Asecurity

To avoid handing out exploit recipes, specific exploitable details are not
enumerated here; please use the private reporting channels above for anything
not already tracked.
