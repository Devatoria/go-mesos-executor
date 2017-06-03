package container

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mesos/mesos-go/api/v1/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type DockerContainerizerTestSuite struct {
	suite.Suite
	dc     *DockerContainerizer
	info   Info
	req    DockerContainerizerTestRequest
	server *httptest.Server
}

type DockerContainerizerTestRequest struct {
	body []byte
	path string
}

func (s *DockerContainerizerTestSuite) SetupTest() {
	var err error

	// Fake server
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read request in order to dump it and to test it later
		body, _ := ioutil.ReadAll(r.Body)
		s.T().Logf("Dumped request is %s", string(body))
		s.req = DockerContainerizerTestRequest{
			body: body,
			path: r.URL.Path,
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}"))
	}))

	// Client
	s.dc, err = NewDockerContainerizer(s.server.URL)
	if err != nil {
		panic(err)
	}

	// Info
	hostPath := "/data"
	protocol := "tcp"
	s.info = Info{
		CPUSharesLimit: 1024,
		MemoryLimit:    512,
		TaskInfo: mesos.TaskInfo{
			Command: &mesos.CommandInfo{
				Environment: &mesos.Environment{
					Variables: []mesos.Environment_Variable{
						mesos.Environment_Variable{
							Name:  "foo",
							Value: "bar",
						},
					},
				},
			},
			Container: &mesos.ContainerInfo{
				Docker: &mesos.ContainerInfo_DockerInfo{
					Network: mesos.ContainerInfo_DockerInfo_HOST.Enum(),
					PortMappings: []mesos.ContainerInfo_DockerInfo_PortMapping{
						mesos.ContainerInfo_DockerInfo_PortMapping{
							ContainerPort: uint32(80),
							HostPort:      uint32(30000),
							Protocol:      &protocol,
						},
					},
				},
				Volumes: []mesos.Volume{
					mesos.Volume{
						ContainerPath: "/usr/share/nginx/html",
						HostPath:      &hostPath,
						Mode:          mesos.RW.Enum(),
					},
				},
			},
		},
	}
}

func (s *DockerContainerizerTestSuite) TestNewDockerContainerizer() {
	assert.NotNil(s.T(), s.dc)
	assert.NotNil(s.T(), s.dc.Client)
}

// Check that:
// - JSON sent to docker daemon is as it should be, containing all the needed fields
// - wrong network mode throws an error
func (s *DockerContainerizerTestSuite) TestDockerContainerCreate() {
	// Expected request JSON:
	//{
	//  "Memory": 512,
	//  "CpuShares": 1024,
	//  "Env": [
	//    "foo=bar"
	//  ],
	//  "Cmd": null,
	//  "Entrypoint": null,
	//  "HostConfig": {
	//    "Binds": [
	//      "/data:/usr/share/nginx/html:rw"
	//    ],
	//    "PortBindings": {
	//      "80/tcp": [
	//        {
	//          "HostPort": "30000"
	//        }
	//      ]
	//    },
	//    "NetworkMode": "host",
	//    "RestartPolicy": {},
	//    "LogConfig": {}
	//  }
	//}
	var result struct {
		CPUShares  uint64 `json:"CpuShares"`
		Env        []string
		Memory     uint64
		HostConfig struct {
			Binds        []string
			NetworkMode  string
			PortBindings map[string][]struct {
				HostPort string
			}
		}
	}

	// Nominal case + request JSON tests
	_, err := s.dc.ContainerCreate(s.info)
	assert.Nil(s.T(), err) // Should be nil (everything is OK)
	if err := json.Unmarshal(s.req.body, &result); err != nil {
		s.T().Fatal(err)
	}

	assert.Equal(s.T(), s.info.MemoryLimit, result.Memory)                                   // Should be equal to the task memory limit
	assert.Equal(s.T(), s.info.CPUSharesLimit, result.CPUShares)                             // Should be equal to the task CPU shares limit
	assert.Equal(s.T(), "host", result.HostConfig.NetworkMode)                               // Should be the string representation of the task network mode
	assert.Equal(s.T(), []string{"foo=bar"}, result.Env)                                     // Should be formated as a list of "key=value" strings
	assert.Equal(s.T(), []string{"/data:/usr/share/nginx/html:rw"}, result.HostConfig.Binds) // Should be formated as a list of "hostPath:containerPath:mode" strings

	portBindings, ok := result.HostConfig.PortBindings["80/tcp"]
	assert.True(s.T(), ok)                                 // Should be present and formated as "port/protocol"
	assert.Equal(s.T(), "30000", portBindings[0].HostPort) // Should be equal to task host port

	// Invalid network mode (should throw an error)
	var invalidNetwork mesos.ContainerInfo_DockerInfo_Network = 666
	s.info.TaskInfo.Container.Docker.Network = &invalidNetwork
	_, err = s.dc.ContainerCreate(s.info)
	assert.NotNil(s.T(), err)
}

// Check that returns nil if everything is ok
func (s *DockerContainerizerTestSuite) TestDockerContainerRun() {
	err := s.dc.ContainerRun("abcdef1234")
	assert.Nil(s.T(), err)
	assert.Empty(s.T(), s.req.body)
}

// Check that returns nil if everything is ok
func (s *DockerContainerizerTestSuite) TestDockerContainerStop() {
	err := s.dc.ContainerStop("abcdef1234")
	assert.Nil(s.T(), err)
	assert.Empty(s.T(), s.req.body)
}

// Check that returns nil if everything is ok
func (s *DockerContainerizerTestSuite) TestDockerContainerRemove() {
	err := s.dc.ContainerRemove("abcdef1234")
	assert.Nil(s.T(), err)
	assert.Empty(s.T(), s.req.body)
}

// Check that returns nil if everything is ok
func (s *DockerContainerizerTestSuite) TestDockerContainerGetPID() {
	_, err := s.dc.ContainerGetPID("abcdef1234")
	assert.Nil(s.T(), err)
	assert.Empty(s.T(), s.req.body)
}

func TestDockerContainerizerSuite(t *testing.T) {
	suite.Run(t, new(DockerContainerizerTestSuite))
}
