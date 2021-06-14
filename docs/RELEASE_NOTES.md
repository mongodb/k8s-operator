# MongoDB Kubernetes Operator 0.6.1

## Kubernetes Operator

- Bug fixes
  - when deleting MongoDB Resource cleanup related resources (k8s services, secrets).
  
- Changes
  - fixed an issue where the operator would reconcile based on events emitted by itself in certain situations.
  - support connection strings using SRV.
  - expose connection strings (including auth/tls values) for deployments as secrets for easy of use. Secrets name template: _\<MongoDB resource name\>-\<db\>-\<user\>_

## MongoDB Agent ReadinessProbe

- Changes
  - Readiness probe now patches pod annotations rather than overwriting them.
  
## Miscellaneous
Ubuntu-based agent images are now based on Ubuntu 20.04 instead of Ubuntu 16.06

## Updated Image Tags

- mongodb-kubernetes-operator:0.6.1
- mongodb-kubernetes-readinessprobe:1.0.4

_All the images can be found in:_

https://quay.io/mongodb
