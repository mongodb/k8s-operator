# Deploy and Configure a MongoDB Resource #

The [`/config/samples`](../config/samples) directory contains example MongoDB resources that you can modify and deploy.

## Table of Contents

- [Deploy a Replica Set](#deploy-a-replica-set)
- [Scale a Replica Set](#scale-a-replica-set)
- [Upgrade your MongoDB Resource Version and Feature Compatibility Version](#upgrade-your-mongodb-resource-version-and-feature-compatibility-version)
  - [Example](#example)
- [Deploy Replica Sets on OpenShift](#deploy-replica-sets-on-openshift)
- [Define a Custom Database Role](#define-a-custom-database-role)

## Deploy a Replica Set

To deploy your first replica set:

1. Replace `<your-password-here>` in [config/samples/mongodb.com_v1_mongodbcommunity_cr.yaml](../config/samples/mongodb.com_v1_mongodbcommunity_cr.yaml) to the password you wish to use.
2. Invoke the following `kubectl` command:
   ```
   kubectl apply -f config/samples/mongodb.com_v1_mongodbcommunity_cr.yaml --namespace <my-namespace>
   ```
3. Verify that the MongoDB resource deployed:
   ```
   kubectl get mongodbcommunity --namespace <my-namespace>
   ```
4. Connect clients to the MongoDB replica set:
   ```
   mongo "mongodb://<service-object-name>.<namespace>.svc.cluster.local:27017/?replicaSet=<replica-set-name>"
   ```
**NOTE**: You can access each `mongod` process in the replica set only from within a pod
running in the cluster.

## Scale a Replica Set

You can scale up (increase) or scale down (decrease) the number of
members in a replica set.

Consider the following example MongoDB resource definition:

```yaml
apiVersion: mongodbcommunity.mongodb.com/v1
kind: MongoDBCommunity
metadata:
  name: example-mongodb
spec:
  members: 3
  type: ReplicaSet
  version: "4.2.7"
```

To scale a replica set:

1. Edit the resource definition.

   Update `members` to the number of members that you want the replica set to have.

   ```yaml
   apiVersion: mongodbcommunity.mongodb.com/v1
   kind: MongoDBCommunity
   metadata:
     name: example-mongodb
   spec:
     members: 5
     type: ReplicaSet
     version: "4.2.7"
   ```

2. Reapply the configuration to Kubernetes:
   ```
   kubectl apply -f <example>.yaml --namespace <my-namespace>
   ```

   **NOTE**: When you scale down a MongoDB resource, the Community Operator
   might take several minutes to remove the StatefulSet replicas for the
   members that you remove from the replica set.

## Upgrade your MongoDB Resource Version and Feature Compatibility Version

You can upgrade the major, minor, and/or feature compatibility versions of your MongoDB resource. These settings are configured in your resource definition YAML file.

- To upgrade your resource's major and/or minor versions, set the `spec.version` setting to the desired MongoDB version.

- To modify your resource's [feature compatibility version](https://docs.mongodb.com/manual/reference/command/setFeatureCompatibilityVersion/), set the `spec.featureCompatibilityVersion` setting to the desired version.

If you update `spec.version` to a later version, consider setting `spec.featureCompatibilityVersion` to the current working MongoDB version to give yourself the option to downgrade if necessary. To learn more about feature compatibility, see [`setFeatureCompatibilityVersion`](https://docs.mongodb.com/manual/reference/command/setFeatureCompatibilityVersion/) in the MongoDB Manual.

### Example

Consider the following example MongoDB resource definition:

```yaml
apiVersion: mongodbcommunity.mongodb.com/v1
kind: MongoDBCommunity
metadata:
  name: example-mongodb
spec:
  members: 3
  type: ReplicaSet
  version: "4.0.6"
```
To upgrade this resource from `4.0.6` to `4.2.7`:

1. Edit the resource definition.

   a. Update `spec.version` to `4.2.7`.

   b. Update `spec.featureCompatibilityVersion` to `4.0`.

   ```yaml
   apiVersion: mongodbcommunity.mongodb.com/v1
   kind: MongoDBCommunity
   metadata:
     name: example-mongodb
   spec:
     members: 3
     type: ReplicaSet
     version: "4.2.7"
     featureCompatibilityVersion: "4.0"
   ```

   **NOTE:** Setting `featureCompatibilityVersion` to `4.0` disables [4.2 features incompatible with MongoDB 4.0](https://docs.mongodb.com/manual/release-notes/4.2-compatibility/#compatibility-enabled).

2. Reapply the configuration to Kubernetes:
   ```
   kubectl apply -f <example>.yaml --namespace <my-namespace>
   ```

## Deploy Replica Sets on OpenShift

To deploy the operator on OpenShift you will have to provide the environment variable `MANAGED_SECURITY_CONTEXT` set to `true` for the operator deployment.

See [here](/config/samples/mongodb.com_v1_mongodbcommunity_openshift_cr.yaml) for
an example of how to provide the required configuration for a MongoDB
replica set.

See [here](../deploy/openshift/operator_openshift.yaml) for an example of how to configure the Operator deployment.

## Define a Custom Database Role

You can define [custom roles](https://docs.mongodb.com/manual/core/security-user-defined-roles/) to give you fine-grained access control over your MongoDB database resource.

  **NOTE**: Custom roles are scoped to a single MongoDB database resource.

To define a custom role:

1. Add the following fields to the MongoDB resource definition:

   | Key | Type | Description | Required? |
   |----|----|----|----|
   | `spec.security.roles` | array | Array that defines [custom roles](https://docs.mongodb.com/manual/core/security-user-defined-roles/) roles that give you fine-grained access control over your MongoDB deployment. | Yes |
   | `spec.security.roles.role` | string | Name of the custom role. | Yes |
   | `spec.security.roles.db` | string | Database in which you want to store the user-defined role. | Yes |
   | `spec.security.roles.authenticationRestrictions` | array | Array that defines the IP address from which and to which users assigned this role can connect. | No |
   | `spec.security.roles.authenticationRestrictions.clientSource` | array | Array of IP addresses or CIDR blocks from which users assigned this role can connect. <br><br> MongoDB servers reject connection requests from users with this role if the requests come from a client that is not present in this array. | No |
   | `spec.security.roles.authenticationRestrictions.serverAddress` | array | Array of IP addresses or CIDR blocks to which users assigned this role can connect. <br><br> MongoDB servers reject connection requests from users with this role if the client requests to connect to a server that is not present in this array. | No |
   | `spec.security.roles.privileges` | array | List of actions that users granted this role can perform. For a list of accepted values, see [Privilege Actions](https://docs.mongodb.com/manual/reference/privilege-actions/#database-management-actions) in the MongoDB Manual for the MongoDB versions you deploy with the Kubernetes Operator. | Yes |
   | `spec.security.roles.privileges.actions` | array | Name of the role. Valid values are [built-in roles](https://docs.mongodb.com/manual/reference/built-in-roles/#built-in-roles). | Yes |
   | `spec.security.roles.privileges.resource.database`| string | Database for which the privilege `spec.security.roles.privileges.actions` apply. An empty string (`""`) indicates that the privilege actions apply to all databases. <br><br> If you provide a value for this setting, you must also provide a value for `spec.security.roles.privileges.resource.collection`. | Conditional |
   | `spec.security.roles.privileges.resource.collection`| string | Collection for which the privilege `spec.security.roles.privileges.actions` apply. An empty string (`""`) indicates that the privilege actions apply to all of the database's collections.<br><br> If you provide a value for this setting, you must also provide a value for `spec.security.roles.privileges.resource.database`. | Conditional |
   | `spec.security.roles.privileges.resource.cluster`| string | Flag that indicates that the privilege `spec.security.roles.privileges.actions` apply to all databases and collections in the MongoDB deployment. If omitted, defaults to `false`.<br><br> If set to `true`, do not provide values for `spec.security.roles.privileges.resource.database` and `spec.security.roles.privileges.resource.collection`. | Conditional |
   | `spec.security.roles.roles`| array | An array of roles from which this role inherits privileges. <br><br> You must include the roles field. Use an empty array (`[]`) to specify no roles to inherit from. | Yes |
   | `spec.security.roles.roles.role` | string | Name of the role to inherit from. | Conditional |
   | `spec.security.roles.roles.database` | string | Name of database that contains the role to inherit from. | Conditional |

   ```yaml
   ---
   apiVersion: mongodbcommunity.mongodb.com/v1
   kind: MongoDBCommunity
   metadata:
     name: custom-role-mongodb
   spec:
     members: 3
     type: ReplicaSet
     version: "4.2.6"
     security:
       authentication:
         modes: ["SCRAM"]
       roles: # custom roles are defined here
         - role: testRole
           db: admin
           privileges:
             - resource:
                 db: "test"
                 collection: "" # an empty string indicates any collection
               actions:
                 - find
           roles: []
     users:
       - name: my-user
         db: admin
         passwordSecretRef: # a reference to the secret that will be used to generate the user's password
           name: my-user-password
         roles:
           - name: clusterAdmin
             db: admin
           - name: userAdminAnyDatabase
             db: admin
           - name: testRole # apply the custom role to the user
             db: admin
         scramCredentialsSecretName: my-scram
   ```

2. Save the file.
3. Apply the updated MongoDB resource definition:

   ```
   kubectl apply -f <mongodb-crd>.yaml --namespace <my-namespace>
   ```
