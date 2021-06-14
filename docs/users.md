# Create a Database User #

You can create a MongoDB database user to authenticate to your MongoDB resource using [SCRAM](https://docs.mongodb.com/manual/core/security-scram/). First, [create a Kubernetes secret](#create-a-user-secret) for the new user's password. Then, [modify and apply the MongoDB resource definition](#modify-the-mongodb-resource).

You cannot disable SCRAM authentication.

## Create a User Secret

1. Copy the following example secret.

     ```yaml
     ---
     apiVersion: v1
     kind: Secret
     metadata:
       name: <db-user-secret>  # corresponds to spec.users.passwordSecretRef.name in the MongoDB CRD
     type: Opaque
     stringData:
       password: <my-plain-text-password> # corresponds to spec.users.passwordSecretRef.key in the MongoDB CRD
     ...
     ```
1. Update the value of `metadata.name` with any name for this secret.
1. Update the value of `stringData.password` with the user's password.
1. Save the secret with a `.yaml` file extension.
1. Apply the secret in Kubernetes:
   ```
   kubectl apply -f <db-user-secret>.yaml --namespace <my-namespace>
   ```

## Modify the MongoDB Resource

1. Add the following fields to the MongoDB resource definition:

   | Key | Type | Description | Required? |
   |----|----|----|----|
   | `spec.users` | array of objects | Configures database users for this deployment. | Yes |
   | `spec.users.name` | string | Username of the database user. | Yes |
   | `spec.users.db` | string | Database that the user authenticates against. Defaults to `admin`. | No |
   | `spec.users.passwordSecretRef.name` | string | Name of the secret that contains the user's plain text password. | Yes|
   | `spec.users.passwordSecretRef.key` | string| Key in the secret that corresponds to the value of the user's password. Defaults to `password`. | No |
   | `spec.users.scramCredentialsSecretName` | string| ScramCredentialsSecretName appended by string "scram-credentials" is the name of the secret object created by the operator for storing SCRAM credentials for the user. The name should comply with [DNS1123 subdomain](https://tools.ietf.org/html/rfc1123). Also, please make sure the name is unique among `users`.  | Yes |
   | `spec.users.roles` | array of objects | Configures roles assigned to the user. | Yes |
   | `spec.users.roles.role.name` | string | Name of the role. Valid values are [built-in roles](https://docs.mongodb.com/manual/reference/built-in-roles/#built-in-roles) and [custom roles](deploy-configure.md#define-a-custom-database-role) that you have defined. | Yes |
   | `spec.users.roles.role.db` | string | Database that the role applies to. | Yes |

   ```yaml
   ---
   apiVersion: mongodbcommunity.mongodb.com/v1
   kind: MongoDBCommunity
   metadata:
     name: example-scram-mongodb
   spec:
     members: 3
     type: ReplicaSet
     version: "4.2.6"
     security:
       authentication:
         modes: ["SCRAM"]
     users:
       - name: <username>
         db: <authentication-database>
         passwordSecretRef:
           name: <db-user-secret>
         roles:
           - name: <role-1>
             db: <role-1-database>
           - name: <role-2>
             db: <role-2-database>
   ...
   ```
2. Save the file.
3. Apply the updated MongoDB resource definition:

   ```
   kubectl apply -f <mongodb-crd>.yaml --namespace <my-namespace>
   ```

## Next Steps

- After the MongoDB resource is running, the Operator no longer requires the user's secret. MongoDB recommends that you securely store the user's password and then delete the user secret:
  ```
  kubectl delete secret <db-user-secret> --namespace <my-namespace>
  ```

- To authenticate to your MongoDB resource, run the following command:
   ```
   mongo "mongodb://<service-object-name>.<my-namespace>.svc.cluster.local:27017/?replicaSet=<replica-set-name>" --username <username> --password <password> --authenticationDatabase <authentication-database>
   ```
- To change a user's password, create and apply a new secret resource definition with a `metadata.name` that is the same as the name specified in `passwordSecretRef.name` of the MongoDB CRD. The Operator will automatically regenerate credentials.
