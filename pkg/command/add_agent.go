package command

import (
	"github.com/choerodon/choerodon-cluster-agent/pkg/agent/model"
	"github.com/choerodon/choerodon-cluster-agent/pkg/command/agent"
)

func init() {
	Funcs.Add(model.InitAgent, agent.InitAgent)

	Funcs.Add(model.ReSyncAgent, agent.ReSyncAgent)

	Funcs.Add(model.CreateEnv, agent.AddEnv)
	Funcs.Add(model.EnvDelete, agent.DeleteEnv)
}
