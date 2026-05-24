#!/bin/sh
set -e
# All INPUT_* env vars are already exported by the GitHub Actions runner.
# The CLI's render command will load them via config.LoadFromEnv when no
# -c flag is passed.
exec /usr/local/bin/gmetrics render
