# Secure MongoDB Resources #

## Table of Contents

- [Secure MongoDB Resource Connections using TLS](#secure-mongodb-resource-connections-using-tls)
  - [Prerequisites](#prerequisites)
  - [Procedure](#procedure)

## Secure MongoDB Resource Connections using TLS

You can configure the MongoDB Community Kubernetes Operator to use TLS certificates to encrypt traffic between:

- MongoDB hosts in a replica set, and
- Client applications and MongoDB deployments.

### Prerequisites

Before you secure MongoDB resource connections using TLS, you must:

1. Create a PEM-encoded TLS certificate for the servers in the MongoDB resource using your own Certificate Authority (CA). The certificate must have one of the following:

   - A wildcard `Common Name` that matches the domain name of all of the replica set members:

     ```
     *.<metadata.name of the MongoDB resource>-svc.<namespace>.svc.cluster.local
     ```
   - The domain name for each of the replica set members as `Subject Alternative Names` (SAN):

     ```
     <metadata.name of the MongoDB resource>-0.<metadata.name of the MongoDB resource>-svc.<namespace>.svc.cluster.local
     <metadata.name of the MongoDB resource>-1.<metadata.name of the MongoDB resource>-svc.<namespace>.svc.cluster.local
     <metadata.name of the MongoDB resource>-2.<metadata.name of the MongoDB resource>-svc.<namespace>.svc.cluster.local
     ```

1. Create a Kubernetes ConfigMap that contains the certificate for the CA that signed your server certificate. The key in the ConfigMap that references the certificate must be named `ca.crt`. Kubernetes configures this automatically if the certificate file is named `ca.crt`:
   ```
   kubectl create configmap <tls-ca-configmap-name> --from-file=ca.crt --namespace <namespace>
   ```

   For a certificate file with any other name, you must define the `ca.crt` key manually:
   ```
   kubectl create configmap <tls-ca-configmap-name> --from-file=ca.crt=<certificate-file-name>.crt --namespace <namespace>
   ```

1. Create a Kubernetes secret that contains the server certificate and key for the members of your replica set. For a server certificate named `server.crt` and key named `server.key`:
   ```
   kubectl create secret tls <tls-secret-name> --cert=server.crt --key=server.key --namespace <namespace>
   ```

### Procedure

To secure connections to MongoDB resources using TLS:

1. Add the following fields to the MongoDB resource definition:

   - `spec.security.tls.enabled`: Encrypts communications using TLS certificates between MongoDB hosts in a replica set and client applications and MongoDB deployments. Set to `true`.
   - `spec.security.tls.optional`: (**Optional**) Enables the members of the replica set to accept both TLS and non-TLS client connections. Equivalent to setting the MongoDB[`net.tls.mode`](https://docs.mongodb.com/manual/reference/configuration-options/#net.tls.mode) setting to `preferSSL`. If omitted, defaults to `false`.

     ---
     **NOTE**

     When you enable TLS on an existing replica set deployment:

     a. Set `spec.security.tls.optional` to `true`.

     b. Apply the configuration to Kubernetes.

     c. Upgrade your existing clients to use TLS.

     d. Remove the `spec.security.tls.optional` field.

     e. Complete the remaining steps in the procedure.

     ---
   - `spec.security.tls.certificateKeySecretRef.name`: Name of the Kubernetes secret that contains the server certificate and key that you created in the [prerequisites](#prerequisites-1).
   - `spec.security.tls.caConfigMapRef.name`: Name of the Kubernetes ConfigMap that contains the Certificate Authority certificate used to sign the server certificate that you created in the [prerequisites](#prerequisites-1).

   ```yaml
   apiVersion: mongodbcommunity.mongodb.com/v1
   kind: MongoDBCommunity
   metadata:
     name: example-mongodb
   spec:
     members: 3
     type: ReplicaSet
     version: "4.2.7"
     security:
       tls:
         enabled: true
         certificateKeySecretRef:
           name: <tls-secret-name>
         caConfigMapRef:
           name: <tls-ca-configmap-name>
   ```

1. Apply the configuration to Kubernetes:
   ```
   kubectl apply -f <example>.yaml --namespace <my-namespace>
   ```
1. From within the Kubernetes cluster, connect to the MongoDB resource.
   - If `spec.security.tls.optional` is omitted or `false`: clients must
     establish TLS connections to the MongoDB servers in the replica set.
   - If `spec.security.tls.optional` is true, clients can establish TLS or
     non-TLS connections to the MongoDB servers in the replica set.

   See the documentation for your connection method to learn how to establish a TLS connection to a MongoDB server.
