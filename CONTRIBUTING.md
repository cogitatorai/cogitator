# Contributing to Cogitator

Thanks for your interest in contributing. Cogitator is maintained by a solo developer, so the process is intentionally lightweight.


## Before you start

**Open a discussion first** for any non-trivial change. This includes new features, architectural changes, and anything that touches the agent loop, memory system, or security model. A quick conversation saves everyone time.

Bug fixes, documentation improvements, and small quality-of-life changes can go straight to a pull request.


## What contributions are welcome

**Yes, please:**
- Bug fixes with a clear reproduction case
- New channel adapters (WhatsApp, Slack, Discord, etc.)
- New connectors (calendar, email, CRM, etc.)
- Dashboard improvements (visualizations, UX, accessibility)
- Documentation and examples
- Test coverage improvements
- Performance optimizations with benchmarks

**Discuss first:**
- Changes to the agent loop or context building
- Changes to the memory system (knowledge graph, retrieval, consolidation)
- Changes to the security model (sandbox, path filtering, credential handling)
- New built-in tools
- Dependency additions

**Not accepting:**
- Features that require external services with no self-hosted alternative
- Changes that break the single-binary deployment model
- Telemetry or analytics that phone home without explicit user consent


## Pull request guidelines

1. **One concern per PR.** A bug fix is one PR. A new feature is another. Mixing them makes review harder.

2. **Write tests** for bug fixes (prove it's fixed) and new functionality. The server uses standard Go testing.

3. **Follow existing patterns.** Read the code around what you're changing. Match the style, naming conventions, and error handling patterns you see.

4. **Keep the commit history clean.** Squash fixup commits before requesting review. A PR with 3 meaningful commits is fine. A PR with 15 "fix typo" commits is not.

5. **Update documentation** if your change affects user-facing behavior, API endpoints, or configuration options.

6. **No generated code in PRs.** If your change requires code generation (protobuf, OpenAPI, etc.), include the generator config and instructions, not the output.


## Response times

This is a solo project. Expect 1 to 2 weeks for PR review. Complex changes may take longer. If your PR has been open for more than 2 weeks without a response, leave a comment and I'll prioritize it.


## Code of conduct

Be respectful. Be constructive. Assume good intent. That covers it.


## License

By contributing, you agree that your contributions will be licensed under the AGPL-3.0 license that covers the project.
