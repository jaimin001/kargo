package stages

import (
	"context"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
	"github.com/akuity/kargo/internal/credentials"
)

func TestNewStageReconciler(t *testing.T) {
	kubeClient := fake.NewClientBuilder().Build()
	e := newReconciler(
		kubeClient,
		kubeClient,
		&credentials.FakeDB{},
	)
	require.NotNil(t, e.kargoClient)
	require.NotNil(t, e.argoClient)
	require.NotNil(t, e.credentialsDB)

	// Assert that all overridable behaviors were initialized to a default:

	// Loop guard:
	require.NotNil(t, e.hasNonTerminalPromotionsFn)

	// Common:
	require.NotNil(t, e.getArgoCDAppFn)

	// Health checks:
	require.NotNil(t, e.checkHealthFn)

	// Syncing:
	require.NotNil(t, e.getLatestFreightFromReposFn)
	require.NotNil(t, e.getAvailableFreightFromUpstreamStagesFn)
	require.NotNil(t, e.getLatestCommitsFn)
	require.NotNil(t, e.getLatestImagesFn)
	require.NotNil(t, e.getLatestTagFn)
	require.NotNil(t, e.getLatestChartsFn)
	require.NotNil(t, e.getLatestChartVersionFn)
	require.NotNil(t, e.getLatestCommitMetaFn)
}

func TestSync(t *testing.T) {
	scheme := k8sruntime.NewScheme()
	require.NoError(t, kargoapi.SchemeBuilder.AddToScheme(scheme))

	noNonTerminalPromotionsFn := func(
		context.Context,
		string,
		string,
	) (bool, error) {
		return false, nil
	}

	testCases := []struct {
		name       string
		stage      *kargoapi.Stage
		reconciler *reconciler
		assertions func(
			initialStatus kargoapi.StageStatus,
			newStatus kargoapi.StageStatus,
			client client.Client,
			err error,
		)
	}{
		{
			name:  "error checking for non-terminal promotions",
			stage: &kargoapi.Stage{},
			reconciler: &reconciler{
				hasNonTerminalPromotionsFn: func(
					context.Context,
					string,
					string,
				) (bool, error) {
					return false, errors.New("something went wrong")
				},
			},
			assertions: func(
				initialStatus kargoapi.StageStatus,
				newStatus kargoapi.StageStatus,
				_ client.Client,
				err error,
			) {
				require.Error(t, err)
				require.Equal(t, "something went wrong", err.Error())
				// Status should be returned unchanged
				require.Equal(t, initialStatus, newStatus)
			},
		},

		{
			name: "non-terminal promotions found",
			stage: &kargoapi.Stage{
				Status: kargoapi.StageStatus{
					CurrentPromotion: &kargoapi.PromotionInfo{
						Name: "dev.abc123.def456",
						Freight: kargoapi.Freight{
							ID: "xyz789",
						},
					},
				},
			},
			reconciler: &reconciler{
				hasNonTerminalPromotionsFn: func(
					context.Context,
					string,
					string,
				) (bool, error) {
					return true, nil
				},
			},
			assertions: func(
				initialStatus kargoapi.StageStatus,
				newStatus kargoapi.StageStatus,
				_ client.Client,
				err error,
			) {
				require.NoError(t, err)
				// Status should be returned unchanged
				require.Equal(t, initialStatus, newStatus)
			},
		},

		{
			name: "no non-terminal promotions found",
			stage: &kargoapi.Stage{
				Spec: &kargoapi.StageSpec{
					Subscriptions: &kargoapi.Subscriptions{},
				},
				Status: kargoapi.StageStatus{
					CurrentPromotion: &kargoapi.PromotionInfo{ // This should get cleared
						Name: "dev.abc123.def456",
						Freight: kargoapi.Freight{
							ID: "xyz789",
						},
					},
				},
			},
			reconciler: &reconciler{
				hasNonTerminalPromotionsFn: noNonTerminalPromotionsFn,
			},
			assertions: func(
				initialStatus kargoapi.StageStatus,
				newStatus kargoapi.StageStatus,
				_ client.Client,
				err error,
			) {
				require.NoError(t, err)
				require.Nil(t, newStatus.CurrentPromotion)
			},
		},

		{
			name: "no subscriptions",
			stage: &kargoapi.Stage{
				Spec: &kargoapi.StageSpec{
					Subscriptions: &kargoapi.Subscriptions{},
				},
			},
			reconciler: &reconciler{
				hasNonTerminalPromotionsFn: noNonTerminalPromotionsFn,
			},
			assertions: func(
				initialStatus kargoapi.StageStatus,
				newStatus kargoapi.StageStatus,
				_ client.Client,
				err error,
			) {
				require.NoError(t, err)
				// Status should be returned unchanged
				require.Equal(t, initialStatus, newStatus)
			},
		},

		{
			name: "error getting latest Freight from repos",
			stage: &kargoapi.Stage{
				Spec: &kargoapi.StageSpec{
					Subscriptions: &kargoapi.Subscriptions{
						Repos: &kargoapi.RepoSubscriptions{},
					},
				},
			},
			reconciler: &reconciler{
				hasNonTerminalPromotionsFn: noNonTerminalPromotionsFn,
				getLatestFreightFromReposFn: func(
					context.Context,
					string,
					kargoapi.RepoSubscriptions,
				) (*kargoapi.Freight, error) {
					return nil, errors.New("something went wrong")
				},
			},
			assertions: func(
				initialStatus kargoapi.StageStatus,
				newStatus kargoapi.StageStatus,
				_ client.Client,
				err error,
			) {
				require.Error(t, err)
				require.Equal(t, "something went wrong", err.Error())
				// Status should be unchanged
				require.Equal(t, initialStatus, newStatus)
			},
		},

		{
			name: "no latest Freight from repos",
			stage: &kargoapi.Stage{
				Spec: &kargoapi.StageSpec{
					Subscriptions: &kargoapi.Subscriptions{
						Repos: &kargoapi.RepoSubscriptions{},
					},
				},
			},
			reconciler: &reconciler{
				hasNonTerminalPromotionsFn: noNonTerminalPromotionsFn,
				getLatestFreightFromReposFn: func(
					context.Context,
					string,
					kargoapi.RepoSubscriptions,
				) (*kargoapi.Freight, error) {
					return nil, nil
				},
			},
			assertions: func(
				initialStatus kargoapi.StageStatus,
				newStatus kargoapi.StageStatus,
				_ client.Client,
				err error,
			) {
				require.NoError(t, err)
				// Status should be returned unchanged
				require.Equal(t, initialStatus, newStatus)
			},
		},

		{
			name: "latest Freight from repos isn't new",
			stage: &kargoapi.Stage{
				Spec: &kargoapi.StageSpec{
					Subscriptions: &kargoapi.Subscriptions{
						Repos: &kargoapi.RepoSubscriptions{},
					},
					PromotionMechanisms: &kargoapi.PromotionMechanisms{},
					// TODO: I'm not sure about this change
					// HealthChecks: &kargoapi.HealthChecks{},
				},
				Status: kargoapi.StageStatus{
					Health: &kargoapi.Health{
						Status: kargoapi.HealthStateHealthy,
					},
					AvailableFreight: []kargoapi.Freight{
						{
							Commits: []kargoapi.GitCommit{
								{
									RepoURL: "fake-url",
									ID:      "fake-commit",
								},
							},
							Images: []kargoapi.Image{
								{
									RepoURL: "fake-url",
									Tag:     "fake-tag",
								},
							},
						},
					},
					CurrentFreight: &kargoapi.Freight{
						Commits: []kargoapi.GitCommit{
							{
								RepoURL: "fake-url",
								ID:      "fake-commit",
							},
						},
						Images: []kargoapi.Image{
							{
								RepoURL: "fake-url",
								Tag:     "fake-tag",
							},
						},
						Qualified: true,
					},
					History: []kargoapi.Freight{
						{
							Commits: []kargoapi.GitCommit{
								{
									RepoURL: "fake-url",
									ID:      "fake-commit",
								},
							},
							Images: []kargoapi.Image{
								{
									RepoURL: "fake-url",
									Tag:     "fake-tag",
								},
							},
							Qualified: true,
						},
					},
				},
			},
			reconciler: &reconciler{
				hasNonTerminalPromotionsFn: noNonTerminalPromotionsFn,
				checkHealthFn: func(
					context.Context,
					*kargoapi.Freight,
					[]kargoapi.ArgoCDAppUpdate,
				) *kargoapi.Health {
					return &kargoapi.Health{
						Status: kargoapi.HealthStateHealthy,
					}
				},
				getLatestFreightFromReposFn: func(
					context.Context,
					string,
					kargoapi.RepoSubscriptions,
				) (*kargoapi.Freight, error) {
					return &kargoapi.Freight{
						Commits: []kargoapi.GitCommit{
							{
								RepoURL: "fake-url",
								ID:      "fake-commit",
							},
						},
						Images: []kargoapi.Image{
							{
								RepoURL: "fake-url",
								Tag:     "fake-tag",
							},
						},
					}, nil
				},
			},
			assertions: func(
				initialStatus kargoapi.StageStatus,
				newStatus kargoapi.StageStatus,
				_ client.Client,
				err error,
			) {
				require.NoError(t, err)
				// Status should be returned unchanged
				require.Equal(t, initialStatus, newStatus)
			},
		},

		{
			name: "error getting available Freight from upstream Stages",
			stage: &kargoapi.Stage{
				Spec: &kargoapi.StageSpec{
					Subscriptions: &kargoapi.Subscriptions{
						UpstreamStages: []kargoapi.StageSubscription{
							{
								Name: "fake-name",
							},
						},
					},
				},
			},
			reconciler: &reconciler{
				hasNonTerminalPromotionsFn: noNonTerminalPromotionsFn,
				getAvailableFreightFromUpstreamStagesFn: func(
					context.Context,
					string,
					[]kargoapi.StageSubscription,
				) ([]kargoapi.Freight, error) {
					return nil, errors.New("something went wrong")
				},
			},
			assertions: func(
				initialStatus kargoapi.StageStatus,
				newStatus kargoapi.StageStatus,
				_ client.Client,
				err error,
			) {
				require.Error(t, err)
				require.Equal(t, "something went wrong", err.Error())
				// Status should be unchanged
				require.Equal(t, initialStatus, newStatus)
			},
		},

		{
			name: "no latest Freight from upstream Stages",
			stage: &kargoapi.Stage{
				Spec: &kargoapi.StageSpec{
					Subscriptions: &kargoapi.Subscriptions{
						UpstreamStages: []kargoapi.StageSubscription{
							{
								Name: "fake-name",
							},
						},
					},
				},
			},
			reconciler: &reconciler{
				hasNonTerminalPromotionsFn: noNonTerminalPromotionsFn,
				getAvailableFreightFromUpstreamStagesFn: func(
					context.Context,
					string,
					[]kargoapi.StageSubscription,
				) ([]kargoapi.Freight, error) {
					return nil, nil
				},
			},
			assertions: func(
				initialStatus kargoapi.StageStatus,
				newStatus kargoapi.StageStatus,
				_ client.Client,
				err error,
			) {
				require.NoError(t, err)
				// Status should be unchanged
				require.Equal(t, initialStatus, newStatus)
			},
		},

		{
			name: "multiple upstream Stages",
			stage: &kargoapi.Stage{
				Spec: &kargoapi.StageSpec{
					Subscriptions: &kargoapi.Subscriptions{
						UpstreamStages: []kargoapi.StageSubscription{
							// Subscribing to multiple upstream Stages should block
							// auto-promotion
							{
								Name: "fake-name",
							},
							{
								Name: "another-fake-name",
							},
						},
					},
				},
			},
			reconciler: &reconciler{
				hasNonTerminalPromotionsFn: noNonTerminalPromotionsFn,
				getAvailableFreightFromUpstreamStagesFn: func(
					context.Context,
					string,
					[]kargoapi.StageSubscription,
				) ([]kargoapi.Freight, error) {
					return []kargoapi.Freight{
						{},
						{},
					}, nil
				},
			},
			assertions: func(
				initialStatus kargoapi.StageStatus,
				newStatus kargoapi.StageStatus,
				_ client.Client,
				err error,
			) {
				require.NoError(t, err)
				// Status should have updated AvailableFreight and otherwise be
				// unchanged
				require.Equal(
					t,
					kargoapi.FreightStack{{}, {}},
					newStatus.AvailableFreight,
				)
				newStatus.AvailableFreight = initialStatus.AvailableFreight
				require.Equal(t, initialStatus, newStatus)
			},
		},

		{
			name: "no promotion policy found",
			stage: &kargoapi.Stage{
				Spec: &kargoapi.StageSpec{
					Subscriptions: &kargoapi.Subscriptions{
						Repos: &kargoapi.RepoSubscriptions{},
					},
				},
			},
			reconciler: &reconciler{
				hasNonTerminalPromotionsFn: noNonTerminalPromotionsFn,
				getLatestFreightFromReposFn: func(
					context.Context,
					string,
					kargoapi.RepoSubscriptions,
				) (*kargoapi.Freight, error) {
					return &kargoapi.Freight{
						Commits: []kargoapi.GitCommit{
							{
								RepoURL: "fake-url",
								ID:      "fake-commit",
							},
						},
						Images: []kargoapi.Image{
							{
								RepoURL: "fake-url",
								Tag:     "fake-tag",
							},
						},
					}, nil
				},
				kargoClient: fake.NewClientBuilder().WithScheme(scheme).Build(),
			},
			assertions: func(
				initialStatus kargoapi.StageStatus,
				newStatus kargoapi.StageStatus,
				client client.Client,
				err error,
			) {
				require.NoError(t, err)
				// Status should have updated AvailableFreight and otherwise be
				// unchanged
				require.Equal(
					t,
					kargoapi.FreightStack{
						{
							Commits: []kargoapi.GitCommit{
								{
									RepoURL: "fake-url",
									ID:      "fake-commit",
								},
							},
							Images: []kargoapi.Image{
								{
									RepoURL: "fake-url",
									Tag:     "fake-tag",
								},
							},
						},
					},
					newStatus.AvailableFreight,
				)
				newStatus.AvailableFreight = initialStatus.AvailableFreight
				require.Equal(t, initialStatus, newStatus)
				// And no Promotion should have been created
				promos := kargoapi.PromotionList{}
				err = client.List(context.Background(), &promos)
				require.NoError(t, err)
				require.Empty(t, promos.Items)
			},
		},

		{
			name: "multiple promotion policies found",
			stage: &kargoapi.Stage{
				Spec: &kargoapi.StageSpec{
					Subscriptions: &kargoapi.Subscriptions{
						Repos: &kargoapi.RepoSubscriptions{},
					},
				},
			},
			reconciler: &reconciler{
				hasNonTerminalPromotionsFn: noNonTerminalPromotionsFn,
				getLatestFreightFromReposFn: func(
					context.Context,
					string,
					kargoapi.RepoSubscriptions,
				) (*kargoapi.Freight, error) {
					return &kargoapi.Freight{
						Commits: []kargoapi.GitCommit{
							{
								RepoURL: "fake-url",
								ID:      "fake-commit",
							},
						},
						Images: []kargoapi.Image{
							{
								RepoURL: "fake-url",
								Tag:     "fake-tag",
							},
						},
					}, nil
				},
				kargoClient: fake.NewClientBuilder().WithScheme(scheme).WithObjects(
					&kargoapi.PromotionPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "fake-policy",
							Namespace: "fake-namespace",
						},
						Stage: "fake-stage",
					},
					&kargoapi.PromotionPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "another-fake-policy",
							Namespace: "fake-namespace",
						},
						Stage: "fake-stage",
					},
				).Build(),
			},
			assertions: func(
				initialStatus kargoapi.StageStatus,
				newStatus kargoapi.StageStatus,
				client client.Client,
				err error,
			) {
				require.NoError(t, err)
				// Status should have updated AvailableFreight and otherwise be
				// unchanged
				require.Equal(
					t,
					kargoapi.FreightStack{
						{
							Commits: []kargoapi.GitCommit{
								{
									RepoURL: "fake-url",
									ID:      "fake-commit",
								},
							},
							Images: []kargoapi.Image{
								{
									RepoURL: "fake-url",
									Tag:     "fake-tag",
								},
							},
						},
					},
					newStatus.AvailableFreight,
				)
				newStatus.AvailableFreight = initialStatus.AvailableFreight
				require.Equal(t, initialStatus, newStatus)
				// And no Promotion should have been created
				promos := kargoapi.PromotionList{}
				err = client.List(context.Background(), &promos)
				require.NoError(t, err)
				require.Empty(t, promos.Items)
			},
		},

		{
			name: "auto-promotion not enabled",
			stage: &kargoapi.Stage{
				Spec: &kargoapi.StageSpec{
					Subscriptions: &kargoapi.Subscriptions{
						Repos: &kargoapi.RepoSubscriptions{},
					},
				},
			},
			reconciler: &reconciler{
				hasNonTerminalPromotionsFn: noNonTerminalPromotionsFn,
				getLatestFreightFromReposFn: func(
					context.Context,
					string,
					kargoapi.RepoSubscriptions,
				) (*kargoapi.Freight, error) {
					return &kargoapi.Freight{
						Commits: []kargoapi.GitCommit{
							{
								RepoURL: "fake-url",
								ID:      "fake-commit",
							},
						},
						Images: []kargoapi.Image{
							{
								RepoURL: "fake-url",
								Tag:     "fake-tag",
							},
						},
					}, nil
				},
				kargoClient: fake.NewClientBuilder().WithScheme(scheme).WithObjects(
					&kargoapi.PromotionPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "fake-policy",
							Namespace: "fake-namespace",
						},
						Stage: "fake-stage",
					},
				).Build(),
			},
			assertions: func(
				initialStatus kargoapi.StageStatus,
				newStatus kargoapi.StageStatus,
				client client.Client,
				err error,
			) {
				require.NoError(t, err)
				// Status should have updated AvailableFreight and otherwise be
				// unchanged
				require.Equal(
					t,
					kargoapi.FreightStack{
						{
							Commits: []kargoapi.GitCommit{
								{
									RepoURL: "fake-url",
									ID:      "fake-commit",
								},
							},
							Images: []kargoapi.Image{
								{
									RepoURL: "fake-url",
									Tag:     "fake-tag",
								},
							},
						},
					},
					newStatus.AvailableFreight,
				)
				newStatus.AvailableFreight = initialStatus.AvailableFreight
				require.Equal(t, initialStatus, newStatus)
				// And no Promotion should have been created
				promos := kargoapi.PromotionList{}
				err = client.List(context.Background(), &promos)
				require.NoError(t, err)
				require.Empty(t, promos.Items)
			},
		},

		{
			name: "auto-promotion enabled",
			stage: &kargoapi.Stage{
				Spec: &kargoapi.StageSpec{
					Subscriptions: &kargoapi.Subscriptions{
						Repos: &kargoapi.RepoSubscriptions{},
					},
				},
			},
			reconciler: &reconciler{
				hasNonTerminalPromotionsFn: noNonTerminalPromotionsFn,
				getLatestFreightFromReposFn: func(
					context.Context,
					string,
					kargoapi.RepoSubscriptions,
				) (*kargoapi.Freight, error) {
					return &kargoapi.Freight{
						Commits: []kargoapi.GitCommit{
							{
								RepoURL: "fake-url",
								ID:      "fake-commit",
							},
						},
						Images: []kargoapi.Image{
							{
								RepoURL: "fake-url",
								Tag:     "fake-tag",
							},
						},
					}, nil
				},
				kargoClient: fake.NewClientBuilder().WithScheme(scheme).WithObjects(
					&kargoapi.PromotionPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "fake-policy",
							Namespace: "fake-namespace",
						},
						Stage:               "fake-stage",
						EnableAutoPromotion: true,
					},
				).Build(),
			},
			assertions: func(
				initialStatus kargoapi.StageStatus,
				newStatus kargoapi.StageStatus,
				client client.Client,
				err error,
			) {
				require.NoError(t, err)
				// Status should have updated AvailableFreight and otherwise be
				// unchanged
				require.Equal(
					t,
					kargoapi.FreightStack{
						{
							Commits: []kargoapi.GitCommit{
								{
									RepoURL: "fake-url",
									ID:      "fake-commit",
								},
							},
							Images: []kargoapi.Image{
								{
									RepoURL: "fake-url",
									Tag:     "fake-tag",
								},
							},
						},
					},
					newStatus.AvailableFreight,
				)
				newStatus.AvailableFreight = initialStatus.AvailableFreight
				require.Equal(t, initialStatus, newStatus)
				// And a Promotion should have been created
				promos := kargoapi.PromotionList{}
				err = client.List(context.Background(), &promos)
				require.NoError(t, err)
				require.Len(t, promos.Items, 1)
			},
		},

		{
			name: "control-flow stage",
			stage: &kargoapi.Stage{
				Spec: &kargoapi.StageSpec{
					Subscriptions: &kargoapi.Subscriptions{
						UpstreamStages: []kargoapi.StageSubscription{
							{
								Name: "upstream",
							},
						},
					},
					PromotionMechanisms: nil,
				},
				Status: kargoapi.StageStatus{
					AvailableFreight: []kargoapi.Freight{
						{
							Commits: []kargoapi.GitCommit{
								{
									RepoURL: "fake-url",
									ID:      "fake-commit",
								},
							},
						},
					},
					CurrentFreight: &kargoapi.Freight{},
				},
			},
			reconciler: &reconciler{
				hasNonTerminalPromotionsFn: noNonTerminalPromotionsFn,
				getAvailableFreightFromUpstreamStagesFn: func(
					context.Context,
					string,
					[]kargoapi.StageSubscription,
				) ([]kargoapi.Freight, error) {
					return nil, nil
				},
			},
			assertions: func(
				initialStatus kargoapi.StageStatus,
				newStatus kargoapi.StageStatus,
				_ client.Client,
				err error,
			) {
				require.NoError(t, err)
				require.Nil(t, newStatus.CurrentFreight)
				require.Len(t, newStatus.History, len(initialStatus.AvailableFreight))
				for i, f := range newStatus.History {
					require.Equal(t, initialStatus.AvailableFreight[i].ID, f.ID)
					require.True(t, f.Qualified)
				}
			},
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.stage.ObjectMeta = metav1.ObjectMeta{
				Name:      "fake-stage",
				Namespace: "fake-namespace",
			}
			// nolint: lll
			newStatus, err := tc.reconciler.syncStage(context.Background(), tc.stage)
			tc.assertions(
				tc.stage.Status,
				newStatus,
				tc.reconciler.kargoClient,
				err,
			)
		})
	}
}

func TestGetLatestFreightFromRepos(t *testing.T) {
	testCases := []struct {
		name               string
		getLatestCommitsFn func(
			context.Context,
			string,
			[]kargoapi.GitSubscription,
		) ([]kargoapi.GitCommit, error)
		getLatestImagesFn func(
			context.Context,
			string,
			[]kargoapi.ImageSubscription,
		) ([]kargoapi.Image, error)
		getLatestChartsFn func(
			context.Context,
			string,
			[]kargoapi.ChartSubscription,
		) ([]kargoapi.Chart, error)
		assertions func(*kargoapi.Freight, error)
	}{
		{
			name: "error getting latest git commit",
			getLatestCommitsFn: func(
				context.Context,
				string,
				[]kargoapi.GitSubscription,
			) ([]kargoapi.GitCommit, error) {
				return nil, errors.New("something went wrong")
			},
			assertions: func(freight *kargoapi.Freight, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "error syncing git repo subscription")
				require.Contains(t, err.Error(), "something went wrong")
			},
		},

		{
			name: "error getting latest images",
			getLatestCommitsFn: func(
				context.Context,
				string,
				[]kargoapi.GitSubscription,
			) ([]kargoapi.GitCommit, error) {
				return nil, nil
			},
			getLatestImagesFn: func(
				context.Context,
				string,
				[]kargoapi.ImageSubscription,
			) ([]kargoapi.Image, error) {
				return nil, errors.New("something went wrong")
			},
			assertions: func(freight *kargoapi.Freight, err error) {
				require.Error(t, err)
				require.Contains(
					t,
					err.Error(),
					"error syncing image repo subscriptions",
				)
				require.Contains(t, err.Error(), "something went wrong")
			},
		},

		{
			name: "error getting latest charts",
			getLatestCommitsFn: func(
				context.Context,
				string,
				[]kargoapi.GitSubscription,
			) ([]kargoapi.GitCommit, error) {
				return nil, nil
			},
			getLatestImagesFn: func(
				context.Context,
				string,
				[]kargoapi.ImageSubscription,
			) ([]kargoapi.Image, error) {
				return nil, nil
			},
			getLatestChartsFn: func(
				context.Context,
				string,
				[]kargoapi.ChartSubscription,
			) ([]kargoapi.Chart, error) {
				return nil, errors.New("something went wrong")
			},
			assertions: func(freight *kargoapi.Freight, err error) {
				require.Error(t, err)
				require.Contains(
					t,
					err.Error(),
					"error syncing chart repo subscriptions",
				)
				require.Contains(t, err.Error(), "something went wrong")
			},
		},

		{
			name: "success",
			getLatestCommitsFn: func(
				context.Context,
				string,
				[]kargoapi.GitSubscription,
			) ([]kargoapi.GitCommit, error) {
				return []kargoapi.GitCommit{
					{
						RepoURL: "fake-url",
						ID:      "fake-commit",
					},
				}, nil
			},
			getLatestImagesFn: func(
				context.Context,
				string,
				[]kargoapi.ImageSubscription,
			) ([]kargoapi.Image, error) {
				return []kargoapi.Image{
					{
						RepoURL: "fake-url",
						Tag:     "fake-tag",
					},
				}, nil
			},
			getLatestChartsFn: func(
				context.Context,
				string,
				[]kargoapi.ChartSubscription,
			) ([]kargoapi.Chart, error) {
				return []kargoapi.Chart{
					{
						RegistryURL: "fake-registry",
						Name:        "fake-chart",
						Version:     "fake-version",
					},
				}, nil
			},
			assertions: func(freight *kargoapi.Freight, err error) {
				require.NoError(t, err)
				require.NotNil(t, freight)
				require.NotEmpty(t, freight.ID)
				require.NotNil(t, freight.FirstSeen)
				// All other fields should have a predictable value
				freight.ID = ""
				freight.FirstSeen = nil
				require.Equal(
					t,
					&kargoapi.Freight{
						Commits: []kargoapi.GitCommit{
							{
								RepoURL: "fake-url",
								ID:      "fake-commit",
							},
						},
						Images: []kargoapi.Image{
							{
								RepoURL: "fake-url",
								Tag:     "fake-tag",
							},
						},
						Charts: []kargoapi.Chart{
							{
								RegistryURL: "fake-registry",
								Name:        "fake-chart",
								Version:     "fake-version",
							},
						},
						Qualified: true,
					},
					freight,
				)
			},
		},
	}
	for _, testCase := range testCases {
		testReconciler := &reconciler{
			getLatestCommitsFn: testCase.getLatestCommitsFn,
			getLatestImagesFn:  testCase.getLatestImagesFn,
			getLatestChartsFn:  testCase.getLatestChartsFn,
		}
		t.Run(testCase.name, func(t *testing.T) {
			testCase.assertions(
				testReconciler.getLatestFreightFromRepos(
					context.Background(),
					"fake-namespace",
					kargoapi.RepoSubscriptions{},
				),
			)
		})
	}
}
