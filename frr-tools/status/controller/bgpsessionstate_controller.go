// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"context"
	"net/netip"
	"reflect"
	"strings"
	"time"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/frr"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

const (
	nodeLabel       = "frrk8s.metallb.io/node"
	peerLabel       = "frrk8s.metallb.io/peer"
	vrfLabel        = "frrk8s.metallb.io/vrf"
	noBFDConfigured = "N/A"
)

type BGPPeersFetcher func() (map[string][]*frr.Neighbor, error)

// BGPSessionStateReconciler reconciles a BGPSessionState object.
type BGPSessionStateReconciler struct {
	client.Client
	BGPPeersFetcher
	Logger       log.Logger
	NodeName     string
	Namespace    string
	DaemonPod    *corev1.Pod
	ResyncPeriod time.Duration
}

type peersStatus map[string]*frrk8sv1beta1.BGPSessionState

func (ps peersStatus) add(s frrk8sv1beta1.BGPSessionState) {
	ps[peerFor(s)] = s.DeepCopy()
}

func (ps peersStatus) hasPeerFor(s frrk8sv1beta1.BGPSessionState) bool {
	_, ok := ps[peerFor(s)]
	return ok
}

func (ps peersStatus) statusFor(peer string) *frrk8sv1beta1.BGPSessionState {
	return ps[labelFormatForNeighbor(peer)]
}

func (ps peersStatus) clone() peersStatus {
	res := peersStatus{}
	for k, v := range ps {
		res[k] = v.DeepCopy()
	}
	return res
}

func (ps peersStatus) remove(peer string) {
	delete(ps, labelFormatForNeighbor(peer))
}

// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=bgpsessionstates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=bgpsessionstates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrnodestates,verbs=get;list;watch
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrnodestates/status,verbs=get
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

func (r *BGPSessionStateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	level.Info(r.Logger).Log("controller", "BGPSessionState", "start reconcile", req.String())
	defer level.Info(r.Logger).Log("controller", "BGPSessionState", "end reconcile", req.String())

	l := frrk8sv1beta1.BGPSessionStateList{}
	err := r.List(ctx, &l, client.MatchingLabels{nodeLabel: r.NodeName})
	if err != nil {
		return ctrl.Result{}, err
	}

	states, duplicates := peersStatusPerVRF(l.Items)
	for _, s := range duplicates {
		err := r.Delete(ctx, &s)
		if err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	neighbors, err := r.BGPPeersFetcher()
	if err != nil {
		return ctrl.Result{}, err
	}
	neighbors = renameDefaultVRF(neighbors)

	toApply, toRemove := r.desiredStatesForExisting(neighbors, states)

	errs := []error{}
	for _, s := range toRemove { // delete the existing statuses that belong to non-existing neighbors
		err := r.Delete(ctx, s)
		if err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, err)
		}
	}

	for _, s := range toApply {
		desiredStatus := s.Status
		_, err := controllerutil.CreateOrPatch(ctx, r.Client, s, func() error {
			err = controllerutil.SetOwnerReference(r.DaemonPod, s, r.Scheme())
			if err != nil {
				return err
			}
			s.Status = desiredStatus
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	}

	if utilerrors.NewAggregate(errs) != nil {
		return ctrl.Result{}, utilerrors.NewAggregate(errs)
	}

	// We use the ResyncPeriod for requeuing the node's FRRNodeState, relying on it being
	// the only non-namespaced resource with the node's name that triggers the reconciliation.
	if req.Name == r.NodeName && req.Namespace == "" {
		return ctrl.Result{RequeueAfter: r.ResyncPeriod}, nil
	}

	return ctrl.Result{}, nil
}

func (r *BGPSessionStateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	p := predicate.NewPredicateFuncs(func(o client.Object) bool {
		return r.filterBGPSessionStateEvent(o) && r.filterFRRNodeStateEvent(o)
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&frrk8sv1beta1.BGPSessionState{}).
		Watches(&frrk8sv1beta1.FRRNodeState{}, &handler.EnqueueRequestForObject{}).
		WithEventFilter(p).
		Complete(r)
}

func (r *BGPSessionStateReconciler) filterBGPSessionStateEvent(o client.Object) bool {
	s, ok := o.(*frrk8sv1beta1.BGPSessionState)
	if !ok {
		return true
	}

	if s.Labels == nil {
		return false
	}

	if nodeFor(*s) != r.NodeName {
		return false
	}

	return true
}

func (r *BGPSessionStateReconciler) filterFRRNodeStateEvent(o client.Object) bool {
	s, ok := o.(*frrk8sv1beta1.FRRNodeState)
	if !ok {
		return true
	}

	if s.Name != r.NodeName {
		return false
	}

	return true
}

func (r *BGPSessionStateReconciler) desiredStatesForExisting(vrfNeighbors map[string][]*frr.Neighbor, states map[string]peersStatus) ([]*frrk8sv1beta1.BGPSessionState, []*frrk8sv1beta1.BGPSessionState) {
	toApply := []*frrk8sv1beta1.BGPSessionState{}
	toRemove := []*frrk8sv1beta1.BGPSessionState{}
	existingStates := map[string]peersStatus{}
	for k, ps := range states {
		existingStates[k] = ps.clone()
	}

	for vrf, neighs := range vrfNeighbors {
		for _, neigh := range neighs {
			var existingState *frrk8sv1beta1.BGPSessionState
			if existingForVRF, ok := existingStates[vrf]; ok {
				existingState = existingForVRF.statusFor(neigh.ID)
				existingForVRF.remove(neigh.ID)
			}
			desiredState := r.desiredStateFor(neigh, vrf, existingState)
			if existingState != nil && reflect.DeepEqual(desiredState.Labels, existingState.Labels) && reflect.DeepEqual(desiredState.Status, existingState.Status) {
				continue
			}
			toApply = append(toApply, desiredState.DeepCopy())
		}
	}

	for _, ps := range existingStates {
		for _, s := range ps {
			toRemove = append(toRemove, s.DeepCopy())
		}
	}

	return toApply, toRemove
}

func (r *BGPSessionStateReconciler) desiredStateFor(neigh *frr.Neighbor, vrf string, existing *frrk8sv1beta1.BGPSessionState) *frrk8sv1beta1.BGPSessionState {
	desired := &frrk8sv1beta1.BGPSessionState{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: r.NodeName + "-",
			Namespace:    r.Namespace,
		},
	}
	if existing != nil {
		desired.ObjectMeta = *existing.ObjectMeta.DeepCopy()
	}
	desired.Labels = map[string]string{
		nodeLabel: r.NodeName,
		peerLabel: labelFormatForNeighbor(neigh.ID),
		vrfLabel:  vrf,
	}
	bfdStatus := neigh.BFDStatus
	if bfdStatus == "" {
		bfdStatus = noBFDConfigured
	}
	desired.Status = frrk8sv1beta1.BGPSessionStateStatus{
		Node:      r.NodeName,
		Peer:      labelFormatForNeighbor(neigh.ID),
		VRF:       vrf,
		BGPStatus: neigh.BGPState,
		BFDStatus: bfdStatus,
	}
	return desired
}

func nodeFor(s frrk8sv1beta1.BGPSessionState) string {
	return s.Labels[nodeLabel]
}

func peerFor(s frrk8sv1beta1.BGPSessionState) string {
	return s.Labels[peerLabel]
}

func vrfFor(s frrk8sv1beta1.BGPSessionState) string {
	return s.Labels[vrfLabel]
}

func labelFormatForNeighbor(id string) string {
	addr, err := netip.ParseAddr(id)
	if err != nil { // can happen in the interface case
		return id
	}
	if addr.Is4() {
		return id
	}
	return strings.ReplaceAll(addr.StringExpanded(), ":", "-") // a label value can't contain ":", and must end with an alphanumeric character
}

// Returns a map with the "default" key set to "". We use this since FRR returns "default" when no VRF is configured.
func renameDefaultVRF[T any](m map[string]T) map[string]T {
	res := map[string]T{}
	for k, v := range m {
		res[k] = v
	}
	res[""] = m["default"]
	delete(res, "default")
	return res
}

// peersStatusPerVRF returns a map representation of vrf -> peers statuses for the given resources +
// a slice for the duplicate resources that did not go into the map.
func peersStatusPerVRF(statuses []frrk8sv1beta1.BGPSessionState) (map[string]peersStatus, []frrk8sv1beta1.BGPSessionState) {
	existing := map[string]peersStatus{}
	duplicates := []frrk8sv1beta1.BGPSessionState{}
	for _, s := range statuses {
		vrf := vrfFor(s)
		if _, ok := existing[vrf]; !ok {
			existing[vrf] = peersStatus{}
		}
		ps := existing[vrf]
		ok := ps.hasPeerFor(s)
		if !ok {
			ps.add(s)
			continue
		}
		duplicates = append(duplicates, s)
	}
	return existing, duplicates
}
