package automationconfig

import (
	"path"
)

type ProcessType string

const (
	Mongod                ProcessType = "mongod"
	DefaultMongoDBDataDir             = "/data"
	DefaultAgentLogPath               = "/var/log/mongodb-mms-automation"
)

type Auth struct {
	// Users is a list which contains the desired users at the project level.
	Users    []MongoDBUser `json:"usersWanted,omitempty"`
	Disabled bool          `json:"disabled"`
	// AuthoritativeSet indicates if the MongoDBUsers should be synced with the current list of Users
	AuthoritativeSet bool `json:"authoritativeSet"`
	// AutoAuthMechanisms is a list of auth mechanisms the Automation Agent is able to use
	AutoAuthMechanisms []string `json:"autoAuthMechanisms,omitempty"`

	// AutoAuthMechanism is the currently active agent authentication mechanism. This is a read only
	// field
	AutoAuthMechanism string `json:"autoAuthMechanism"`
	// DeploymentAuthMechanisms is a list of possible auth mechanisms that can be used within deployments
	DeploymentAuthMechanisms []string `json:"deploymentAuthMechanisms,omitempty"`
	// AutoUser is the MongoDB Automation Agent user, when x509 is enabled, it should be set to the subject of the AA's certificate
	AutoUser string `json:"autoUser,omitempty"`
	// Key is the contents of the KeyFile, the automation agent will ensure this a KeyFile with these contents exists at the `KeyFile` path
	Key string `json:"key,omitempty"`
	// KeyFile is the path to a keyfile with read & write permissions. It is a required field if `Disabled=false`
	KeyFile string `json:"keyfile,omitempty"`
	// KeyFileWindows is required if `Disabled=false` even if the value is not used
	KeyFileWindows string `json:"keyfileWindows,omitempty"`
	// AutoPwd is a required field when going from `Disabled=false` to `Disabled=true`
	AutoPwd string `json:"autoPwd,omitempty"`
}

func DisabledAuth() Auth {
	return Auth{
		Users:                    make([]MongoDBUser, 0),
		AutoAuthMechanisms:       make([]string, 0),
		DeploymentAuthMechanisms: make([]string, 0),
		AutoAuthMechanism:        "MONGODB-CR",
		Disabled:                 true,
	}
}

type MongoDBUser struct {
}

type Process struct {
	Name              string      `json:"name"`
	HostName          string      `json:"hostname"`
	Args26            Args26      `json:"args2_6"`
	Replication       Replication `json:"replication"`
	Storage           Storage     `json:"storage"`
	ProcessType       ProcessType `json:"processType"`
	Version           string      `json:"version"`
	AuthSchemaVersion int         `json:"authSchemaVersion"`
	SystemLog         SystemLog   `json:"systemLog"`
	WiredTiger        WiredTiger  `json:"wiredTiger"`
}

type SystemLog struct {
	Destination string `json:"destination"`
	Path        string `json:"path"`
}

func newProcess(name, hostName, version, replSetName string) Process {
	return Process{
		Name:     name,
		HostName: hostName,
		Storage: Storage{
			DBPath: DefaultMongoDBDataDir,
		},
		Replication: Replication{ReplicaSetName: replSetName},
		ProcessType: Mongod,
		Version:     version,
		SystemLog: SystemLog{
			Destination: "file",
			Path:        path.Join(DefaultAgentLogPath, "/mongodb.log"),
		},
		AuthSchemaVersion: 5,
	}
}

type Replication struct {
	ReplicaSetName string `json:"replSetName"`
}

type Storage struct {
	DBPath     string     `json:"dbPath"`
	WiredTiger WiredTiger `json:"wiredTiger"`
}

type WiredTiger struct {
	EngineConfig EngineConfig `json:"engineConfig"`
}

type EngineConfig struct {
	CacheSizeGB float32 `json:"cacheSizeGB"`
}

type LogRotate struct {
	SizeThresholdMB  int `json:"sizeThresholdMB"`
	TimeThresholdHrs int `json:"timeThresholdHrs"`
}

type Args26 struct {
	Net         Net         `json:"net"`
	Security    Security    `json:"security"`
	Replication Replication `json:"replication"`
}

type Net struct {
	Port int `json:"port"`
}

type Security struct {
	ClusterAuthMode string `json:"clusterAuthMode,omitempty"`
}

type ReplicaSet struct {
	Id              string             `json:"_id"`
	Members         []ReplicaSetMember `json:"members"`
	ProtocolVersion string             `json:"protocolVersion"`
}

type ReplicaSetMember struct {
	Id          string `json:"_id"`
	Host        string `json:"host"`
	Priority    int    `json:"priority"`
	ArbiterOnly bool   `json:"arbiterOnly"`
	Votes       int    `json:"votes"`
}

func newReplicaSetMember(p Process, id string) ReplicaSetMember {
	return ReplicaSetMember{
		Id:          id,
		Host:        p.Name,
		Priority:    1,
		ArbiterOnly: false,
		Votes:       1,
	}
}

type AutomationConfig struct {
	Version     int          `json:"version"`
	Processes   []Process    `json:"processes"`
	ReplicaSets []ReplicaSet `json:"replicaSets"`
	Auth        Auth         `json:"auth"`
	Versions    []Version    `json:"versions"`
	Name        string       `json:"name"`
}

type Version struct {
	Builds []Build `json:"builds"`
}

type Build struct {
	Architecture string `json:"architecture"`
	GitVersion   string `json:"gitVersion"`
	Platform     string `json:"platform"`
	URL          string `json:"url"`
	Win2008Plus  bool   `json:"win2008plus,omitempty"`
}
