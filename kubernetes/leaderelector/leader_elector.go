package leaderelector

import (
	"context"
	"fmt"
	"time"

	"github.com/Trendyol/go-dcp-client/membership/info"

	"github.com/Trendyol/go-dcp-client/logger"

	dcpModel "github.com/Trendyol/go-dcp-client/identity"

	"github.com/Trendyol/go-dcp-client/helpers"
	"github.com/Trendyol/go-dcp-client/kubernetes"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

type LeaderElector interface {
	Run(ctx context.Context)
}

type Handler interface {
	OnBecomeLeader()
	OnResignLeader()
	OnBecomeFollower(leaderIdentity *dcpModel.Identity)
}

type leaderElector struct {
	client             kubernetes.Client
	myIdentity         *dcpModel.Identity
	handler            Handler
	leaseLockName      string
	leaseLockNamespace string
}

func (le *leaderElector) Run(ctx context.Context) {
	callback := leaderelection.LeaderCallbacks{
		OnStartedLeading: func(c context.Context) {
			logger.Debug("granted to leader")

			le.client.AddLabel(le.leaseLockNamespace, "role", "leader")

			le.handler.OnBecomeLeader()
		},
		OnStoppedLeading: func() {
			logger.Debug("revoked from leader")

			le.client.RemoveLabel(le.leaseLockNamespace, "role")

			le.handler.OnResignLeader()
		},
		OnNewLeader: func(leaderIdentityStr string) {
			leaderIdentity := dcpModel.NewIdentityFromStr(leaderIdentityStr)

			if le.myIdentity.Equal(leaderIdentity) {
				return
			}

			logger.Debug("granted to follower for leader: %s", leaderIdentity.Name)

			le.client.AddLabel(le.leaseLockNamespace, "role", "follower")

			le.handler.OnBecomeFollower(leaderIdentity)
		},
	}

	go func() {
		leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
			Lock: &resourcelock.LeaseLock{
				LeaseMeta: metaV1.ObjectMeta{
					Name:      le.leaseLockName,
					Namespace: le.leaseLockNamespace,
				},
				Client: le.client.CoordinationV1(),
				LockConfig: resourcelock.ResourceLockConfig{
					Identity: le.myIdentity.String(),
				},
			},
			ReleaseOnCancel: true,
			LeaseDuration:   8 * time.Second,
			RenewDeadline:   5 * time.Second,
			RetryPeriod:     1 * time.Second,
			Callbacks:       callback,
		})
	}()
}

func NewLeaderElector(
	client kubernetes.Client,
	config helpers.ConfigLeaderElection,
	myIdentity *dcpModel.Identity,
	handler Handler,
	infoHandler info.Handler,
) LeaderElector {
	var leaseLockName string
	var leaseLockNamespace string

	if val, ok := config.Config["leaseLockName"]; ok {
		leaseLockName = val
	} else {
		logger.Panic(fmt.Errorf("leaseLockName is not defined"), "error while creating leader elector")
	}

	if val, ok := config.Config["leaseLockNamespace"]; ok {
		leaseLockNamespace = val
	} else {
		logger.Panic(fmt.Errorf("leaseLockNamespace is not defined"), "error while creating leader elector")
	}

	le := &leaderElector{
		client:             client,
		myIdentity:         myIdentity,
		handler:            handler,
		leaseLockName:      leaseLockName,
		leaseLockNamespace: leaseLockNamespace,
	}

	infoHandler.Subscribe(func(new *info.Model) {
		client.AddLabel(leaseLockNamespace, "member", fmt.Sprintf("%v_%v", new.MemberNumber, new.TotalMembers))
	})

	return le
}
