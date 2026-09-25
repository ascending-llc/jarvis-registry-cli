# Contributing

Use Go 1.24.11 or later. Install the repository's pre-commit hook before making changes:

```sh
pre-commit install -t pre-commit
```

Use the [Makefile](Makefile) targets for development tasks. Before opening a pull request, run:

```sh
make all
```

This runs the test suite and configured lint checks.