import os
import shutil
import yaml
from typing import Dict, TextIO
import k8s_request_data


def clean_nones(value: Dict) -> Dict:
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
    sst = k8s_request_data.get_stateful_sets_namespaced(namespace)
    if sst is not None:
        diagnostic_file.write(header("Stateful Sets"))
        diagnostic_file.write(yaml.dump(clean_nones(sst)))


def dump_pod_log_namespaced(namespace: str, name: str, containers: list) -> None:
    for container in containers:
        with open(
            f"logs/e2e/{name}-{container.name}.log", mode="w", encoding="utf-8",
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


def dump_configmaps_namespaced(namespace: str) -> None:
    configmaps = k8s_request_data.get_configmaps_namespaced(namespace)
    if configmaps is not None:
        for configmap_item in configmaps:
            name = configmap_item.metadata.name
            with open(
                f"logs/e2e/ConfigMap-{name}.txt", mode="w", encoding="utf-8"
            ) as log_file:
                configmap = k8s_request_data.get_configmap_namespaced(namespace, name)
                if configmap is not None:
                    log_file.write(yaml.dump(clean_nones(configmap)))


def dump_all(namespace: str) -> None:

    if os.path.exists("logs"):
        shutil.rmtree("logs")

    os.makedirs("logs")

    if not os.path.exists("logs/e2e"):
        os.makedirs("logs/e2e")

    with open(
        "logs/e2e/diagnostics.txt", mode="w", encoding="utf-8"
    ) as diagnostic_file:
        dump_persistent_volume(diagnostic_file)
        dump_stateful_sets_namespaced(diagnostic_file, namespace)
        dump_pods_and_logs_namespaced(diagnostic_file, namespace)

    with open("logs/e2e/crd.log", mode="w", encoding="utf-8") as crd_log:
        dump_crd(crd_log)

    dump_configmaps_namespaced(namespace)
