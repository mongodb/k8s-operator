#!/usr/bin/env python3

import json
import sys
import yaml
from typing import Dict

RELATIVE_PATH_TO_MANAGER_YAML = "config/manager/manager.yaml"
RELATIVE_PATH_TO_OPENSHIFT_MANAGER_YAML = "deploy/openshift/operator_openshift.yaml"


def _load_yaml_file(path: str) -> Dict:
    with open(path, "r") as f:
        return yaml.safe_load(f.read())


def _dump_yaml(operator: Dict, path: str) -> None:
    with open(path, "w+") as f:
        yaml.dump(operator, f)


def update_and_write_file(path: str) -> None:
    release = _load_release()
    yaml_file = _load_yaml_file(path)
    _update_operator_deployment(yaml_file, release)
    _dump_yaml(yaml_file, path)


def _load_release() -> Dict:
    with open("release.json", "r") as f:
        return json.loads(f.read())


def _replace_tag(image: str, new_tag: str) -> str:
    split_image = image.split(":")
    return split_image[0] + ":" + new_tag


def _update_operator_deployment(operator_deployment: Dict, release: Dict) -> None:
    operator_container = operator_deployment["spec"]["template"]["spec"]["containers"][
        0
    ]
    operator_container["image"] = _replace_tag(
        operator_container["image"], release["mongodb-kubernetes-operator"]
    )
    operator_envs = operator_container["env"]
    for env in operator_envs:
        if env["name"] == "VERSION_UPGRADE_HOOK_IMAGE":
            env["value"] = _replace_tag(env["value"], release["version-upgrade-hook"])
        if env["name"] == "READINESS_PROBE_IMAGE":
            env["value"] = _replace_tag(env["value"], release["readiness-probe"])
        if env["name"] == "AGENT_IMAGE":
            env["value"] = _replace_tag(
                env["value"], release["mongodb-agent"]["version"]
            )


def main() -> int:
    update_and_write_file(RELATIVE_PATH_TO_MANAGER_YAML)
    update_and_write_file(RELATIVE_PATH_TO_OPENSHIFT_MANAGER_YAML)
    return 0


if __name__ == "__main__":
    sys.exit(main())
