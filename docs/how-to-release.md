
## How to Release

* Ensure the versions specified in [release.json](../release.json) are correct. 
    * Prepare a PR to bump and versions as required. 

* Ensure that [the release notes](./RELEASE_NOTES.md) are up to date for this release.
    * Review the [tickets for this release](https://jira.mongodb.org/issues?jql=project%20%3D%20CLOUDP%20AND%20component%20%20%3D%20"Kubernetes%20Community"%20%20AND%20status%20in%20(Resolved%2C%20Closed)%20AND%20fixVersion%20%3D%20kube-community-0.6.0%20) (ensure relevant fix version is in the jql query)

* Run the `Create Release PR` GitHub Action
    * In the GitHub UI:
        * `Actions` > `Create Release PR` > `Run Workflow` (on master)
        
* Review and Approve the release PR that is created by this action.
    * Upon approval, all new images for this release will be built and released, and a Github release draft will be created.

* Review and publish the new GitHub release draft.
