---
name: not-in-remote-and-name-collide-skill-3
description: This represents a remote skill whose name collides with a different local skill's outdated name.
allowed-tools:
    - Bash
    - Read
user-invocable: true
---

This represents the case where a stale local folder of the same name is deleted before this skill is (re)created at `version` 6.
