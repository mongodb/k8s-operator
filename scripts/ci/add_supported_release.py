#!/usr/bin/env python3

import argparse

import datetime
import json
import logging
import os
import subprocess
import sys
from typing import Dict, Any

import pymongo

LOGLEVEL = os.environ.get("LOGLEVEL", "INFO").upper()
logging.basicConfig(level=LOGLEVEL)

VALID_IMAGES = frozenset(["mongodb-agent"])


def get_repo_root() -> str:
    output = subprocess.check_output("git rev-parse --show-toplevel".split())

    return output.decode("utf-8").strip()


def get_release() -> Dict[str, Any]:
    release_file = os.path.join(get_repo_root(), "release.json")
    return json.load(open(release_file))


def get_atlas_connection_string() -> str:
    return os.environ["ATLAS_CONNECTION_STRING"]


def mongo_client() -> pymongo.MongoClient:
    cnx_str = get_atlas_connection_string()
    return pymongo.MongoClient(cnx_str)


def add_release_version(image: str, version: str) -> None:
    client = mongo_client()

    database = os.environ["ATLAS_DATABASE"]
    collection = client[database][image]

    year_from_now = datetime.datetime.now() + datetime.timedelta(days=365)

    existing_entry = collection.find_one({"version": version})
    if existing_entry is not None:
        logging.info("Entry for version {} already present".format(version))
        return

    result = collection.insert_one(
        {
            "released_on": datetime.datetime.now(),
            "version": version,
            "supported": True,
            "eol": year_from_now,
            "variants": ["ubi", "ubuntu"],
        }
    )

    logging.info(
        "Added new supported version: {} (id: {})".format(version, result.inserted_id)
    )


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--image-name", help="image to add a new supported version", type=str
    )
    args = parser.parse_args()

    if args.image_name not in VALID_IMAGES:
        print(
            "Image {} not supported. Not adding release version.".format(
                args.image_name
            )
        )
        return 0

    # for now, there is just one version to add as a supported release.
    version = get_release()[args.image_name]["version"]
    logging.info("Adding new release: {} {}".format(args.image_name, version))

    add_release_version(args.image_name, version)

    return 0


if __name__ == "__main__":
    sys.exit(main())
