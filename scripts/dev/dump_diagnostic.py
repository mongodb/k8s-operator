import os
import shutil
import yaml
from typing import Dict, TextIO, List, Union
from base64 import b64decode
import json
import k8s_request_data


def clean_nones(value: Dict) -> Union[List, Dict]:
    """
    Recursively remove all None values from dictionaries and lists, and returns
    the result as a new dictionary or list.
    """
    if isinstance(value, list):
        return [clean_nones(x) for x in value if x is not None]
    if isinstance(value, dict):
        return {key: clean_nones(val) for key, val in value.items() if val is not None}
    return value


def header(msg: str) -> str:
    dashes = (
        "----------------------------------------------------------------------------"
    )
    return f"\n{dashes}\n{msg}\n{dashes}\n"


def dump_crd(crd_log: TextIO) -> None:
    crd = k8s_request_data.get_crds()
    if crd is not None:
        crd_log.write(header("CRD"))
        crd_log.write(yaml.dump(clean_nones(crd)))


def dump_persistent_volume(diagnostic_file: TextIO) -> None:
    pv = k8s_request_data.get_persistent_volumes()
    if pv is not None:
        diagnostic_file.write(header("Persistent Volumes"))
        diagnostic_file.write(yaml.dump(clean_nones(pv)))


def dump_stateful_sets_namespaced(diagnostic_file: TextIO, namespace: str) -> None:
    sts = k8s_request_data.get_stateful_sets_namespaced(namespace)
    if sts is not None:
        diagnostic_file.write(header("Stateful Sets"))
        diagnostic_file.write(yaml.dump(clean_nones(sts)))


def dump_mongodbcommunity_namespaced(diagnostic_file: TextIO, namespace: str) -> None:
    mdb = k8s_request_data.get_all_mongodb_namespaced(namespace)
    if mdb is not None:
        diagnostic_file.write(header("MongoDBCommunity"))
        diagnostic_file.write(yaml.dump(clean_nones(mdb)))


def dump_pod_log_namespaced(namespace: str, name: str, containers: list) -> None:
    for container in containers:
        with open(
            f"logs/e2e/{name}-{container.name}.log",
            mode="w",
            encoding="utf-8",
        ) as log_file:
            log = k8s_request_data.get_pod_log_namespaced(
                namespace, name, container.name
            )
            if log is not None:
                log_file.write(log)


def dump_pods_and_logs_namespaced(diagnostic_file: TextIO, namespace: str) -> None:
    pods = k8s_request_data.get_pods_namespaced(namespace)
    if pods is not None:
        for pod in pods:
            name = pod.metadata.name
            diagnostic_file.write(header(f"Pod {name}"))
            diagnostic_file.write(yaml.dump(clean_nones(pod.to_dict())))
            dump_pod_log_namespaced(namespace, name, pod.spec.containers)


def dump_secret_keys_namespaced(
    namespace: str, keys: List[str], secret_name: str
) -> None:
    secret = k8s_request_data.get_secret_namespaced(namespace, secret_name)
    if secret is not None:
        for key in keys:
            with open(
                f"logs/e2e/{secret_name}-{key}",
                mode="w",
                encoding="utf-8",
            ) as log_file:
                if key in secret["data"]:
                    decoded_data = _decode_secret(secret["data"])
                    log_file.write(json.dumps(json.loads(decoded_data[key]), indent=4))


def _decode_secret(data: Dict[str, str]) -> Dict[str, str]:
    return {k: b64decode(v).decode("utf-8") for (k, v) in data.items()}


def dump_automation_configs(namespace: str) -> None:
    mongodb_resources = k8s_request_data.get_all_mongodb_namespaced(namespace)
    if mongodb_resources is None:
        print("No MongoDB resources found, not dumping any automation configs")
        return
    for mdb in mongodb_resources:
        name = mdb["metadata"]["name"]
        dump_secret_keys_namespaced(
            namespace, ["cluster-config.json"], f"{name}-config"
        )


def dump_all(namespace: str) -> None:
    if os.path.exists("logs"):
        shutil.rmtree("logs")

    os.makedirs("logs")

    if not os.path.exists("logs/e2e"):
        os.makedirs("logs/e2e")

    with open(
        "logs/e2e/diagnostics.txt", mode="w", encoding="utf-8"
    ) as diagnostic_file:
        dump_mongodbcommunity_namespaced(diagnostic_file, namespace)
        dump_persistent_volume(diagnostic_file)
        dump_stateful_sets_namespaced(diagnostic_file, namespace)
        dump_pods_and_logs_namespaced(diagnostic_file, namespace)

    with open("logs/e2e/crd.log", mode="w", encoding="utf-8") as crd_log:
        dump_crd(crd_log)

    dump_automation_configs(namespace)
