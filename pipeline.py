import argparse
import json
import sys
from typing import Dict, Optional

from sonar.sonar import process_image

from scripts.dev.dev_config import load_config, DevConfig

VALID_IMAGE_NAMES = frozenset(
    [
        "agent-ubi",
        "agent-ubuntu",
        "readiness-probe-init",
        "version-post-start-hook-init",
    ]
)

DEFAULT_IMAGE_TYPE = "ubuntu"
DEFAULT_NAMESPACE = "default"


def build_agent_image_ubi(config: DevConfig) -> None:
    image_name = "agent-ubi"
    with open("release.json") as f:
        release = json.loads(f.read())
    args = {
        "agent_version": release["agent"]["version"],
        "tools_version": release["agent"]["tools_version"],
        "tools_distro": "rhel70-x86_64",
        "agent_distro": "rhel7_x86_64",
        "registry": config.repo_url,
    }

    config.ensure_tag("ubi")

    sonar_build_image(
        image_name,
        config,
        args=args,
    )


def build_agent_image_ubuntu(config: DevConfig) -> None:
    image_name = "agent-ubuntu"
    with open("release.json") as f:
        release = json.loads(f.read())
    args = {
        "agent_version": release["agent"]["version"],
        "tools_version": release["agent"]["tools_version"],
        "tools_distro": "ubuntu1604-x86_64",
        "agent_distro": "linux_x86_64",
        "registry": config.repo_url,
    }

    config.ensure_tag("ubuntu")

    sonar_build_image(
        image_name,
        config,
        args=args,
    )


def build_readiness_probe_image(config: DevConfig) -> None:
    with open("release.json") as f:
        release = json.loads(f.read())

    config.ensure_tag("readiness-probe")

    sonar_build_image(
        "readiness-probe-init",
        config,
        args={
            "registry": config.repo_url,
            "release_version": release["readiness-probe"],
        },
    )


def build_version_post_start_hook_image(config: DevConfig) -> None:
    with open("release.json") as f:
        release = json.loads(f.read())

    config.ensure_tag("post-start-hook")

    sonar_build_image(
        "version-post-start-hook-init",
        config,
        args={
            "registry": config.repo_url,
            "release_version": release["version-upgrade-hook"],
        },
    )


def sonar_build_image(
    image_name: str,
    config: DevConfig,
    args: Optional[Dict[str, str]] = None,
    inventory: str = "inventory.yaml",
) -> None:
    """Calls sonar to build `image_name` with arguments defined in `args`."""
    process_image(
        image_name,
        build_args=args,
        inventory=inventory,
        include_tags=config.include_tags,
        skip_tags=config.skip_tags,
    )


def _parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--image-name", type=str)
    parser.add_argument("--release", action="store_true")
    return parser.parse_args()


def main() -> int:
    args = _parse_args()

    image_name = args.image_name
    if image_name not in VALID_IMAGE_NAMES:
        print(
            f"Image name [{image_name}] is not valid. Must be one of [{', '.join(VALID_IMAGE_NAMES)}]"
        )
        return 1

    config = load_config()

    # specify --release to release the image
    if args.release:
        config.ensure_tag("release")

    image_build_function = {
        "agent-ubi": build_agent_image_ubi,
        "agent-ubuntu": build_agent_image_ubuntu,
        "readiness-probe-init": build_readiness_probe_image,
        "version-post-start-hook-init": build_version_post_start_hook_image,
    }[image_name]

    image_build_function(config)
    return 0


if __name__ == "__main__":
    sys.exit(main())
