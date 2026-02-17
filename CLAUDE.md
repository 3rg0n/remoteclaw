# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**WCC (Webex Command and Control)** — A remote system control tool that allows users to manage a local machine via a Webex bot, powered by AI.

### How It Works

1. A backend Webex MCP (Message Control Protocol) remote server handles traffic routing
2. The user registers a token, runs WCC locally, and adds the token to connect
3. The user talks to a Webex bot (e.g., "restart service")
4. An AI engine interprets the command, executes it on the local machine, and reports back
5. Essentially "remote hands" for system administration via chat

## Project Status

This project is in the **planning/pre-implementation** stage. See `.plan/concept.txt` for the original concept.

## Architecture (Planned Components)

- **Local WCC Agent** — Runs on the user's system; receives commands from the backend, executes them locally via AI, and reports results
- **Webex Bot Interface** — User-facing chat interface in Webex
- **WMCP Backend** — Webex MCP remote server that routes traffic between the bot and local WCC agents
- **AI Engine** — Interprets natural language commands into system operations
