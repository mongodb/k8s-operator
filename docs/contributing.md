# Contributing to MongoDB Kubernetes Operator

First you need to get familiar with the [Architecture guide](architecture.md), which explains
from a high perspective how everything works together.

After our experience building the [Enterprise MongoDB Kubernetes
Operator](https://github.com/mongodb/mongodb-enterprise-operator), we have
realized that is is very important to have a clean environment to work, and as such we have
adopted a strategy that makes it easier for everyone to contribute.

This strategy is based on using
[`envtest`](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest) for setting up the tests
and `go test` for running the tests, and making the test-runner itself run as a Kubernetes Pod. This
makes it easier to run the tests in environments with access to a Kubernetes
cluster with no go toolchain installed locally, making it easier to reproduce
our local working environments in CI/CD systems.

# High-Perspective Architecture

The operator itself consists of 1 image, that has all the operational logic to deploy and
maintain the MongoDB resources in your cluster.

The operator deploys MongoDB in Pods (via a higher-level resource, a
StatefulSet), on each Pod there will be multiple images coexisting during the
lifetime of the MongoDB server.

* Agent image: This image includes a binary provided by MongoDB that handles
the local operation of a MongoDB server given a series of configurations
provided by the operator. The configuration exists as a ConfigMap that's created
by the operator and mounted in the Agent's Pod.

* MongoDB image: Docker image that includes the MongoDB server.

* Version upgrade post-start hook image: This image includes a binary that helps orchestrate the
  restarts of the MongoDB Replica Set members, in particular, when dealing with
  version upgrades, which requires a very precise set of operations to allow for
  seamless upgrades and downgrades, with no downtime.

Each Pod holds a member of a Replica Set, and each Pod has different components,
each one of them in charge of some part of the lifecycle of the MongoDB database.

# Getting Started

## PR Prerequisites
* Please ensure you have signed our Contributor Agreement. You can find it [here](https://www.mongodb.com/legal/contributor-agreement). 

* Please ensure that all commits are signed.

## Developer Configuration

The operator is built using `golang`. We use a simple
json file that describe some local options that you need to set for the testing environment
to be able to run properly. Create a json file with the following content:

```json
{
  "namespace": "default",
  "repo_url": "localhost:5000",
  "operator_image": "mongodb-kubernetes-operator",
  "e2e_image": "community-e2e",
  "version_upgrade_hook_image": "community-operator-version-upgrade-post-start-hook",
  "agent_image_ubuntu": "mongodb-agent-dev",
  "agent_image_ubi": "mongodb-agent-ubi-dev",
  "readiness_probe_image": "mongodb-kubernetes-readiness",
  "s3_bucket": ""
}
```

#### Config Options

1. `namespace` is the namespace that will be used by scripts/tooling. All the resources will be deployed here.
2. `repo_url` the repository that should be used to push/pull all images.
3. `operator_image` will be used as the name of the operator deployment, and the name of the operator image when build.
4. `e2e_image` the name of e2e test image that will be built.
5. `version_upgrade_hook_image` the name of the version upgrade post start hook image.
6. `image_type` this can be either `ubi` or `ubuntu` and determines the distro of the images built. (currently only the agent image has multiple distros)
7. `agent_image_ubuntu` the name of the ubuntu agent image.
8. `agent_image_ubi` the name of the ubi agent image.
9. `s3_bucket` the S3 bucket that Dockerfiles will be pushed to as part of the release process. Note: this is only required when running the release tasks locally.


You can set the `MONGODB_COMMUNITY_CONFIG` environment variable to be the absolute path of this file. 
It will default to `~/.community-operator-dev/config.json`

Please see [here](./build_operator_locally.md) to see how to build and deploy the operator locally.

## Configure Docker registry

The build process consists of multiple Docker images being built. You need to specify
where you want the locally built images to be pushed. The Docker registry needs to be
accessible from your Kubernetes cluster.

## Test Namespace

You can change the namespace used for tests, if you are using `Kind`, for
instance, you can leave this as `default`.

## Python Environment

The test runner is a Python script, in order to use it a virtualenv needs to be
created.
  
### Pip
```sh
python -m venv venv
source venv/bin/activate
python -m pip install -r requirements.txt
```

### Pipenv

* create a python environment and install dependencies.
```bash
pipenv install
```

* activate the python environment.
```bash
pipenv shell
```


# Deploying the Operator

In order to deploy the Operator from source, you can run the following command.

```sh
make operator-image deploy
```

This will build and deploy the operator to namespace specified in your configuration file.


#### See the operator deployment
```sh
kubectl get pods
```

#### (Optional) Create a MongoDBCommunity Resource

Follow the steps outlined [here](./deploy-configure.md) to deploy some resource.

#### Cleanup
To remove the operator and any created resources you can run

```sh
make undeploy
```


# Running Tests

### Unit tests

Unit tests should be run from the root of the project with:

```sh
make test
```

### E2E Tests

If this is the first time running E2E tests, you will need to ensure that you have built and pushed
all images required by the E2E tests. You can do this by running.

```sh
make all-images
```

For subsequent tests you can use

```sh
make e2e-k8s test=<test-name>
```

This will only re-build the e2e test image.

We have built a simple mechanism to run E2E tests on your cluster using a runner
that deploys a series of Kubernetes objects, runs them, and awaits for their
completion. If the objects complete with a Success status, it means that the
tests were run successfully.

The available tests can be found in the `tests/e2e` directory, at the time of this
writing we have:

```sh
$ ls -l test/e2e
replica_set
replica_set_change_version
replica_set_readiness_probe
replica_set_scale
...
```

The tests should run individually using the runner like this:

```sh
make e2e-k8s test=<test-name>
```

This will run the `replica_set` E2E test which is a simple test which installs a
MongoDB Replica Set and asserts that the deployed server can be connected to.


### Run the test locally with go test & Telepresence
```sh
make e2e-telepresence test=<test-name>
```

This method uses telepresence to allow connectivity as if your local machine is in the kubernetes cluster,
there will be full MongoDB connectivity using `go test` locally.

Note: you must install [telepresence](https://www.getambassador.io/docs/telepresence/latest/quick-start/) before using this method.

If on MacOS, you can run `make install-prerequisites-macos` which will perform the installation.

## Troubleshooting
When you run a test locally, if the `e2e-test` pod is present, you will have to
first manually delete it; failing to do so will cause the `e2e-test` pod to fail.

# Writing new E2E tests

You can start with the `replica_set` test as a starting point to write a new test.
The tests are written using `envtest` and they are run using `go test`.

Adding a new test is as easy as creating a new directory in `test/e2e` with the
new E2E test, and to run them:

```sh
make e2e test=<test-name>
```

# Before Committing your code

## Set up pre-commit hooks
To set up the pre-commit hooks, please create symbolic links from the provided [hooks](https://github.com/mongodb/mongodb-kubernetes-operator/tree/master/scripts/git-hooks):

* Navigate to your `.git/hooks` directory:

	`cd .git/hooks`

* Create a symlink for every file in the `scripts/git-hooks` directory:

	`ln -s -f  ../../scripts/git-hooks/* .`
