import os
import json
import yaml
import sys
import typing

from kubernetes.client.rest import ApiException
from kubernetes import client


def clean_nones(value):
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
    return "\n{0}\n{1}\n{0}\n".format(dashes, msg)


def dump_crd(crd_log: typing.TextIO):
    crdv1 = client.ApiextensionsV1beta1Api()
    try:
        crd_log.write(header("CRD"))
        mdb = crdv1.list_custom_resource_definition(pretty="true")
        crd_log.write(yaml.dump(clean_nones(mdb.to_dict())))
    except ApiException as e:
        print("Exception when calling list_custom_resource_definition: %s\n" % e)


def dump_persistent_volume(diagnosticFile: typing.TextIO):
    corev1 = client.CoreV1Api()
    try:
        diagnosticFile.write(header("Persistent Volume Claims"))
        mdb = corev1.list_persistent_volume(pretty="true")
        diagnosticFile.write(yaml.dump(clean_nones(mdb.to_dict())))
    except ApiException as e:
        print("Exception when calling list_persistent_volume %s\n" % e)


def dump_stateful_sets_namespaced(diagnosticFile: typing.TextIO, namespace: str):
    av1beta1 = client.AppsV1Api()
    try:
        diagnosticFile.write(header("Stateful Sets"))
        mdb = av1beta1.list_namespaced_stateful_set(namespace, pretty="true")
        diagnosticFile.write(yaml.dump(clean_nones(mdb.to_dict())))
    except ApiException as e:
        print("Exception when calling list_namespaced_stateful_set: %s\n" % e)


def dump_pod_log_namespaced(namespace: str, name: str):
    corev1 = client.CoreV1Api()
    try:
        if name.startswith("mdb0"):
            pod_file_mongoDB_agent = open(
                "logs/e2e/{}-mongodb-agent.log".format(name), "w"
            )
            pod_file_mongod = open("logs/e2e/{}-mongod.log".format(name), "w")
            log_mongodb_agent = corev1.read_namespaced_pod_log(
                name=name, namespace=namespace, pretty="true", container="mongodb-agent"
            )
            log_mongod = corev1.read_namespaced_pod_log(
                name=name, namespace=namespace, pretty="true", container="mongod"
            )
            pod_file_mongoDB_agent.write(log_mongodb_agent)
            pod_file_mongod.write(log_mongod)
            pod_file_mongod.close()
            pod_file_mongoDB_agent.close()
        elif name.startswith("mongodb-kubernetes-operator"):
            pod_file = open("logs/e2e/{}.log".format(name), "w")
            log = corev1.read_namespaced_pod_log(
                name=name, namespace=namespace, pretty="true"
            )
            pod_file.write(log)
            pod_file.close()
    except ApiException as e:
        print("Exception when calling read_namespaced_pod_log: %s\n" % e)


def dump_pods_and_logs_namespaced(diagnosticFile: typing.TextIO, namespace: str):
    corev1 = client.CoreV1Api()
    try:
        diagnosticFile.write(header("Pods"))
        pods = corev1.list_namespaced_pod(namespace)
        for pod in pods.items:
            name = pod.metadata.name
            diagnosticFile.write(header("Pod {}".format(name)))
            diagnosticFile.write(yaml.dump(clean_nones(pod.to_dict())))
            dump_pod_log_namespaced(namespace, name)
    except ApiException as e:
        print("Exception when calling list_namespaced_pod: %s\n" % e)


def dump_all(namespace: str):

    if not os.path.exists("logs"):
        os.makedirs("logs")

    if not os.path.exists("logs/e2e"):
        os.makedirs("logs/e2e")

    diagnostic_file = open("logs/e2e/diagnostics.txt", "w")
    crd_log = open("logs/e2e/crd.log", "w")

    dump_crd(crd_log)
    dump_persistent_volume(diagnostic_file)
    dump_stateful_sets_namespaced(diagnostic_file, namespace)
    dump_pods_and_logs_namespaced(diagnostic_file, namespace)

    diagnostic_file.close()
    crd_log.close()
