# Project goals
1. Lightweight coding client with these core functions:
- Read, write, edit files.
- Bash/shell access.
- Ability to run sudo with permissions given.
2. CLI invokation. This tool should allow other agents, processes or scripts to call GoHome to trigger an agent session.
3. Subagent delegation and handling. This tool must account for subagent workflows and proper navigation of sessions. No nesting allowed, just one layer.
4. This tool is designed for local LLM usage - which means possibly slow responses from the LLM endpoint. Design with that in mind.
5. This tool should aim to consume minimal amounts of CPU/RAM.
6. Web search (pending)