package agent

import (
	"encoding/json"
	"fmt"
	"github.com/choerodon/choerodon-cluster-agent/pkg/agent/model"
	"github.com/choerodon/choerodon-cluster-agent/pkg/gitops"
	"github.com/choerodon/choerodon-cluster-agent/pkg/helm"
	"github.com/choerodon/choerodon-cluster-agent/pkg/kube"
	"github.com/choerodon/choerodon-cluster-agent/pkg/operator"
	commandutil "github.com/choerodon/choerodon-cluster-agent/pkg/util/command"
	"github.com/choerodon/choerodon-cluster-agent/pkg/util/controller"
	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"math/rand"
	"time"
)

func InitAgent(opts *commandutil.Opts, cmd *model.Packet) ([]*model.Packet, *model.Packet) {

	var agentInitOpts model.AgentInitOptions
	err := json.Unmarshal([]byte(cmd.Payload), &agentInitOpts)
	if err != nil {
		return nil, commandutil.NewResponseError(cmd.Key, model.InitAgentFailed, err)
	}

	namespaces := opts.Namespaces

	g := gitops.New(opts.Wg, opts.GitConfig, opts.GitRepos, opts.KubeClient, opts.Cluster, opts.CrChan)
	g.GitHost = agentInitOpts.GitHost

	if err := g.PrepareSSHKeys(agentInitOpts.Envs, opts); err != nil {
		return nil, commandutil.NewResponseError(cmd.Key, model.InitAgentFailed, err)
	}

	nsList := []string{}
	for _, envPara := range agentInitOpts.Envs {
		nsList = append(nsList, envPara.Namespace)
		ns, _ := createNamespace(opts.KubeClient, envPara.Namespace)
		if ns == nil {
			glog.V(1).Infof("create namespace %s failed", envPara)
		}
	}
	namespaces.Set(nsList)

	//启动控制器， todo: 重启metrics
	opts.ControllerContext.ReSync()

	cfg, err := opts.KubeClient.GetRESTConfig()
	if err != nil {
		return nil, commandutil.NewResponseError(cmd.Key, model.InitAgentFailed, err)
	}

	args := &controller.Args{
		CrChan:       opts.CrChan,
		HelmClient:   opts.HelmClient,
		Namespaces:   namespaces,
		KubeClient:   opts.KubeClient,
		PlatformCode: opts.PlatformCode,
	}

	testManagerNamespace := "choerodon-test"
	nsList = append(nsList, testManagerNamespace)
	for _, ns := range nsList {
		if opts.Mgrs.IsExist(ns) {
			continue
		}
		mgr, err := operator.New(cfg, ns, args)
		if err != nil {
			return nil, commandutil.NewResponseError(cmd.Key, model.InitAgentFailed, err)
		}
		stopCh := make(chan struct{}, 1)
		// check success added avoid repeat watch
		if opts.Mgrs.AddStop(ns, mgr, stopCh) {
			go func() {
				if err := mgr.Start(stopCh); err != nil {
					opts.CrChan.ResponseChan <- commandutil.NewResponseError(cmd.Key, model.InitAgentFailed, err)
				}
			}()
		}
	}

	g.Envs = agentInitOpts.Envs
	go g.WithStop(opts.StopCh)

	return nil, nil
}

// 以前用于重新部署实例，现在仅用于升级Agent
func UpgradeAgent(opts *commandutil.Opts, cmd *model.Packet) ([]*model.Packet, *model.Packet) {
	var req helm.UpgradeReleaseRequest
	err := json.Unmarshal([]byte(cmd.Payload), &req)
	if err != nil {
		return nil, commandutil.NewResponseErrorWithCommit(cmd.Key, req.Commit, model.HelmReleaseInstallFailed, err)
	}
	if req.Namespace == "" {
		req.Namespace = cmd.Namespace()
	}

	ch := opts.CrChan
	resp, err := opts.HelmClient.UpgradeRelease(&req)
	if err != nil {
		if req.ChartName == "choerodon-cluster-agent" && req.Namespace == "choerodon" {
			go func() {
				//maybe avoid lot request devOps-service in a same time
				rand.Seed(time.Now().UnixNano())
				randWait := rand.Intn(20)
				time.Sleep(time.Duration(randWait) * time.Second)
				glog.Infof("start retry upgrade agent ...")
				ch.CommandChan <- cmd
			}()
		}
		return nil, commandutil.NewResponseErrorWithCommit(cmd.Key, req.Commit, model.HelmReleaseInstallFailed, err)
	}
	respB, err := json.Marshal(resp)
	if err != nil {
		return nil, commandutil.NewResponseErrorWithCommit(cmd.Key, req.Commit, model.HelmReleaseInstallFailed, err)
	}
	return nil, &model.Packet{
		Key:     cmd.Key,
		Type:    model.HelmReleaseUpgradeResourceInfo,
		Payload: string(respB),
	}
}

func ReSyncAgent(opts *commandutil.Opts, cmd *model.Packet) ([]*model.Packet, *model.Packet) {
	fmt.Println("get command re_sync")
	opts.ControllerContext.ReSync()
	return nil, nil
}

func createNamespace(kubeClient kube.Client, namespace string) (*v1.Namespace, error) {
	return kubeClient.GetKubeClient().CoreV1().Namespaces().Create(&v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	})
}
