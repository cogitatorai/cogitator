# Cogitator

**Your AI that actually learns.**

A self-hosted AI agent that remembers what you tell it, works while you sleep, and gets smarter the longer you use it. Runs on your computer. Keeps your data private.

Available on macOS, iOS, and Android. Windows and Linux coming soon.

## Memory that connects

Other AI assistants remember bits and pieces about you. Cogitator builds a graph: your preferences connect to your habits, your habits connect to your history, and relationships between ideas surface automatically.

Memory is shared across users. When one household member tells the agent about a family trip, everyone's assistant knows about it. When a teammate documents a decision, the whole team benefits. Each person controls what stays private and what gets shared.

Pin critical facts so they're always available. Over time, the agent consolidates patterns and stops asking questions it already knows the answer to.

## Scheduled tasks

Schedule tasks that run automatically on a recurring basis. Generate a morning briefing that pulls data from multiple sources (your calendar, your Gmail account, weather, and more). Generate weekly reports. Send yourself a notification when something breaks. Every scheduled task has access to the same tools and memory the agent uses in conversation.

## Self-healing

When a task fails, the agent reads the failure log, figures out what went wrong, and decides how to fix it. If the problem is temporary (a timeout, a connection drop), it backs off and retries. If the problem is structural (a broken prompt, a misconfigured skill), it rewrites the task or updates the skill so the next run succeeds. The agent respects all security boundaries and will never work around a restriction to make a task pass.

## Connectors

Google Calendar and Gmail are built in, with more connectors coming soon. You can also use MCP to connect additional tools through a growing ecosystem of compatible servers. Slack, databases, APIs. The agent discovers new tools automatically and figures out when to use them.

## Privacy first

Being a native app means your conversations, memories, and data never leave your machine. Secrets are stored securely by your operating system, not in plaintext files. Commands run in a sandboxed environment with credential scrubbing.

You choose the AI provider. Cloud models, local models, or a mix: a capable model for hard problems, a cheaper one for routine work.

## Multi-user

Multiple users can share a single instance. Each person gets their own sessions, tasks, and private memories. Role-based access keeps things organized. Invite codes make onboarding simple.

## Cross-platform

**macOS.** A native desktop app with auto-updates and notifications. Runs as a local server or connects to an existing instance.

**iOS and Android.** A mobile companion with push notifications, file attachments, and real-time chat.

## Extensible

**Skills.** Browse and install capabilities from ClawHub, an open marketplace of agent skills built on the OpenClaw standard. The agent can search for skills, install them, and use them immediately. It can also write its own.

**Custom tools.** Define new tools with a name, description, and command template. Drop the file in your workspace and it's available in the next conversation.

**MCP servers.** Connect compatible tool servers for instant access to external ecosystems. The agent auto-generates documentation for every connected server.

## Install

Download the latest release from the [Releases](https://github.com/cogitatorai/cogitator/releases) page.

Extract `Cogitator.app` and move it to `/Applications`.

If macOS shows "app is damaged", run:

```
xattr -cr /Applications/Cogitator.app
```

Launch the app, configure your AI provider, and start a conversation.

## License

Cogitator is proprietary software. All rights reserved.
