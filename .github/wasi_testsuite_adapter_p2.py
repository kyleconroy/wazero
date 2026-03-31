# adapter for wazero WASI Preview 2 testsuite
# This adapter runs WASI p2 component tests using the wazero CLI with
# component model support.

import argparse
import subprocess
import sys
import os
import shlex

# shlex.split() splits according to shell quoting rules
WAZERO = shlex.split(os.getenv("TEST_RUNTIME_EXE", "wazero"))

parser = argparse.ArgumentParser()
parser.add_argument("--version", action="store_true")
parser.add_argument("--test-file", action="store")
parser.add_argument("--arg", action="append", default=[])
parser.add_argument("--env", action="append", default=[])
parser.add_argument("--dir", action="append", default=[])
parser.add_argument("--wasi", action="store", default="preview2",
                    help="WASI version to use (preview2 or preview3)")

args = parser.parse_args()

if args.version:
    version = subprocess.run(
        WAZERO + ["version"], capture_output=True, text=True
    ).stdout.strip()
    if version == "dev":
        version = "0.0.0"
    print("wazero", version)
    sys.exit(0)

TEST_FILE = args.test_file
TEST_DIR = os.path.dirname(TEST_FILE)
PROG_ARGS = []
if args.arg:
    PROG_ARGS = ["--"] + args.arg
ENV_ARGS = [f"-env={e}" for e in args.env]
cwd = os.getcwd()
DIR_ARGS = [f"-mount={cwd}/{dir}:{dir}" for dir in args.dir]

# Use the --wasi flag to select the WASI version
WASI_FLAG = f"-wasi={args.wasi}"

PROG = (
    WAZERO
    + ["run", WASI_FLAG, "-hostlogging=filesystem"]
    + ENV_ARGS
    + DIR_ARGS
    + [TEST_FILE]
    + PROG_ARGS
)
sys.exit(subprocess.run(PROG, cwd=TEST_DIR).returncode)
