#!/usr/bin/env python3

# This script accepts a key from the "release.json" file.
# If the corresponding image of the specified version has been released,

import json
import sys
from typing import List, Dict

import requests

# contains a map of the quay urls to fetch data about the corresponding images.
QUAY_URL_MAP = {
    "mongodb-agent": "https://quay.io/api/v1/repository/mongodb/mongodb-agent-ubi",
    "readiness-probe": "https://quay.io/api/v1/repository/mongodb/mongodb-kubernetes-readinessprobe",
    "version-upgrade-hook": "https://quay.io/api/v1/repository/mongodb/mongodb-kubernetes-operator-version-upgrade-post-start-hook",
    "mongodb-kubernetes-operator": "https://quay.io/api/v1/repository/mongodb/mongodb-kubernetes-operator",
}


def _get_all_released_tags(image_type: str) -> List[str]:
    url = QUAY_URL_MAP[image_type]
    resp = requests.get(url).json()
    tags = resp["tags"]
    return list(tags.keys())


def _load_image_name_to_version_map() -> Dict:
    with open("release.json") as f:
        release = json.loads(f.read())

    # agent section is a sub object, we change the mapping so the key corresponds to the version directly.
    release["mongodb-agent"] = release["mongodb-agent"]["version"]
    return release


def main() -> int:
    if len(sys.argv) != 2:
        raise ValueError("usage: determine_required_releases.py [image-name]")
    image_name_map = _load_image_name_to_version_map()

    if sys.argv[1] not in image_name_map:
        raise ValueError(
            "Unknown image type [{}], valid values are [{}]".format(
                sys.argv[1], ",".join(image_name_map.keys())
            )
        )

    if sys.argv[1] not in QUAY_URL_MAP:
        raise ValueError("No associated image url with key [{}]".format(sys.argv[1]))

    tags = _get_all_released_tags(sys.argv[1])
    if image_name_map[sys.argv[1]] in tags:
        print("released")
    else:
        print("unreleased")
    return 0


if __name__ == "__main__":
    sys.exit(main())