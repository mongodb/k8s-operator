#!/usr/bin/env bash

PY_VERSION=`python3 --version`
echo $PY_VERSION
python3 -m ensurepip --default-pip
pip3 install -r ./requirements.txt
SKIP_CLEANUP="1" python3 ./scripts/dev/e2e.py --skip-image-build 1 --test ${test:?} --tag ${version_id:?} --config_file ./scripts/ci/config.json
