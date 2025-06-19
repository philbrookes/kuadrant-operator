package utils

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	v1 "github.com/kuadrant/kuadrant-operator/api/v1"
	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
	exttypes "github.com/kuadrant/kuadrant-operator/pkg/extension/types"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
)

type clientKeyType struct{}
type schemeKeyType struct{}

var ClientKey = clientKeyType{}
var SchemeKey = schemeKeyType{}

func LoggerFromContext(ctx context.Context) logr.Logger {
	return controller.LoggerFromContext(ctx)
}

func ClientFromContext(ctx context.Context) (client.Client, error) {
	client, ok := ctx.Value(ClientKey).(client.Client)
	if !ok {
		return nil, errors.New("failed to retrieve the client from context")
	}
	return client, nil
}

func SchemeFromContext(ctx context.Context) (*runtime.Scheme, error) {
	scheme, ok := ctx.Value(SchemeKey).(*runtime.Scheme)
	if !ok {
		return nil, errors.New("failed to retrieve scheme from context")
	}
	return scheme, nil
}

func MapToExtPolicy(p exttypes.Policy) *extpb.Policy {
	targetRefs := lo.Map(p.GetTargetRefs(), func(targetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName, _ int) *extpb.TargetRef {
		return &extpb.TargetRef{
			Group:       string(targetRef.Group),
			Kind:        string(targetRef.Kind),
			Name:        string(targetRef.Name),
			SectionName: string(ptr.Deref(targetRef.SectionName, "")),
		}
	})
	return &extpb.Policy{
		Metadata: &extpb.Metadata{
			Group:     p.GetObjectKind().GroupVersionKind().Group,
			Kind:      p.GetObjectKind().GroupVersionKind().Kind,
			Namespace: p.GetNamespace(),
			Name:      p.GetName(),
		},
		TargetRefs: targetRefs,
	}
}

func GatewayToProtobuf(g *machinery.Gateway) *extpb.Gateway {
	listeners := []*extpb.Listener{}
	for _, l := range g.Spec.Listeners {
		listeners = append(listeners, &extpb.Listener{
			Name:     string(l.Name),
			Hostname: string(*l.Hostname),
		})
	}
	addresses := []*extpb.GatewayAddresses{}
	for _, a := range g.Spec.Addresses {
		aType := ""
		if a.Type != nil {
			aType = string(*a.Type)
		}
		addresses = append(addresses, &extpb.GatewayAddresses{
			AddressType: aType,
			Value:       a.Value,
		})
	}
	statusListeners := []*extpb.ListenerStatus{}
	for _, l := range g.Status.Listeners {
		lConds := []*extpb.Condition{}
		for _, c := range l.Conditions {
			lConds = append(lConds, &extpb.Condition{
				Type:               c.Type,
				ConditionStatus:    string(c.Status),
				ObservedGeneration: c.ObservedGeneration,
				Reason:             c.Reason,
				Message:            c.Message,
			})
		}
		statusListeners = append(statusListeners, &extpb.ListenerStatus{
			Name:           string(l.Name),
			AttachedRoutes: l.AttachedRoutes,
			Conditions:     lConds,
		})
	}
	statusAddresses := []*extpb.GatewayAddresses{}
	for _, a := range g.Status.Addresses {
		aType := ""
		if a.Type != nil {
			aType = string(*a.Type)
		}
		statusAddresses = append(statusAddresses, &extpb.GatewayAddresses{
			AddressType: aType,
			Value:       a.Value,
		})
	}
	extConds := []*extpb.Condition{}
	for _, c := range g.Status.Conditions {
		extConds = append(extConds, &extpb.Condition{
			Type:               c.Type,
			ConditionStatus:    string(c.Status),
			ObservedGeneration: c.ObservedGeneration,
			Reason:             c.Reason,
			Message:            c.Message,
		})
	}
	return &extpb.Gateway{
		Metadata: &extpb.Metadata{
			Group:     g.GroupVersionKind().Group,
			Kind:      g.Kind,
			Name:      g.Name,
			Namespace: g.Namespace,
		},
		Spec: &extpb.GatewaySpec{
			GatewayClassName: string(g.Spec.GatewayClassName),
			Listeners:        listeners,
			Addresses:        addresses,
		},
		Status: &extpb.GatewayStatus{
			Addresses:  statusAddresses,
			Conditions: extConds,
			Listeners:  statusListeners,
		},
	}
}

func DNSPolicyToProtobuf(dnsPolicy *v1.DNSPolicy) *extpb.Policy {
	extConds := []*extpb.Condition{}
	for _, c := range dnsPolicy.Status.Conditions {
		extConds = append(extConds, &extpb.Condition{
			Type:               c.Type,
			ConditionStatus:    string(c.Status),
			ObservedGeneration: c.ObservedGeneration,
			Reason:             c.Reason,
			Message:            c.Message,
		})
	}

	targetRef := &extpb.TargetRef{}
	targetRef.Group = string(dnsPolicy.Spec.TargetRef.Group)
	targetRef.Kind = string(dnsPolicy.Spec.TargetRef.Kind)
	targetRef.Name = string(dnsPolicy.Spec.TargetRef.Name)
	if dnsPolicy.Spec.TargetRef.SectionName != nil {
		targetRef.SectionName = string(*dnsPolicy.Spec.TargetRef.SectionName)
	}

	return &extpb.Policy{
		Metadata: &extpb.Metadata{
			Group:     dnsPolicy.GroupVersionKind().Group,
			Kind:      dnsPolicy.GroupVersionKind().Kind,
			Name:      dnsPolicy.Name,
			Namespace: dnsPolicy.Namespace,
		},
		TargetRefs: []*extpb.TargetRef{targetRef},
		Status: &extpb.PolicyStatus{
			ObservedGeneration: dnsPolicy.Status.ObservedGeneration,
			Conditions:         extConds,
		},
	}
}

func DNSRecordToProtobuf(dnsRecord *kuadrantdnsv1alpha1.DNSRecord) *extpb.DNSRecord {
	endpoints := []*extpb.DNSRecordEndpoint{}
	for _, e := range dnsRecord.Spec.Endpoints {
		labels := []*extpb.DNSRecordLabel{}
		for n, v := range e.Labels {
			labels = append(labels, &extpb.DNSRecordLabel{
				Name:  n,
				Value: v,
			})
		}
		providerSpecifics := []*extpb.DNSRecordProviderSpecific{}
		for _, p := range e.ProviderSpecific {
			providerSpecifics = append(providerSpecifics, &extpb.DNSRecordProviderSpecific{
				Name:  p.Name,
				Value: p.Value,
			})
		}
		endpoints = append(endpoints, &extpb.DNSRecordEndpoint{
			DnsName:          e.DNSName,
			RecordTTL:        int64(e.RecordTTL),
			RecordType:       e.RecordType,
			Targets:          e.Targets,
			Labels:           labels,
			ProviderSpecific: providerSpecifics,
			SetIdentifier:    e.SetIdentifier,
		})
	}

	healthchecks := &extpb.DNSRecordHealthCheck{}
	if dnsRecord.Spec.HealthCheck != nil {
		healthchecks = &extpb.DNSRecordHealthCheck{
			Path:             dnsRecord.Spec.HealthCheck.Path,
			FailureThreshold: int64(dnsRecord.Spec.HealthCheck.FailureThreshold),
			Port:             int64(dnsRecord.Spec.HealthCheck.Port),
			Protocol:         string(dnsRecord.Spec.HealthCheck.Protocol),
		}
	}

	extConds := []*extpb.Condition{}
	for _, c := range dnsRecord.Status.Conditions {
		extConds = append(extConds, &extpb.Condition{
			Type:               c.Type,
			ConditionStatus:    string(c.Status),
			ObservedGeneration: c.ObservedGeneration,
			Reason:             c.Reason,
			Message:            c.Message,
		})
	}

	return &extpb.DNSRecord{
		Metadata: &extpb.Metadata{
			Group:     dnsRecord.GroupVersionKind().Group,
			Kind:      dnsRecord.GroupVersionKind().Kind,
			Name:      dnsRecord.Name,
			Namespace: dnsRecord.Namespace,
		},
		Spec: &extpb.DNSRecordSpec{
			Endpoints:   endpoints,
			HealthCheck: healthchecks,
			RootHost:    dnsRecord.Spec.RootHost,
			ProviderRef: &extpb.DNSRecordProviderRef{
				Name: dnsRecord.Spec.ProviderRef.Name,
			},
		},
		Status: &extpb.DNSRecordStatus{
			ObservedGeneration: dnsRecord.Status.ObservedGeneration,
			Conditions:         extConds,
			QueuedAt:           dnsRecord.Status.QueuedAt.Unix(),
			WriteCounter:       dnsRecord.Status.WriteCounter,
			OwnerID:            dnsRecord.Status.OwnerID,
			DomainOwners:       dnsRecord.Status.DomainOwners,
			ZoneID:             dnsRecord.Status.ZoneID,
			ZoneDomainName:     dnsRecord.Status.ZoneDomainName,
		},
	}
}

func DNSHealthCheckToProtobuf(healthCheck *kuadrantdnsv1alpha1.DNSHealthCheckProbe) *extpb.DNSHealthCheckProbe {
	additionalHeadersRef := &extpb.DNSHealthCheckAdditionalHeadersRef{}
	if healthCheck.Spec.AdditionalHeadersRef != nil {
		additionalHeadersRef.Name = healthCheck.Spec.AdditionalHeadersRef.Name
	}
	healthy := false
	if healthCheck.Status.Healthy != nil {
		healthy = *healthCheck.Status.Healthy
	}
	return &extpb.DNSHealthCheckProbe{
		Metadata: &extpb.Metadata{
			Group:     healthCheck.GroupVersionKind().Group,
			Kind:      healthCheck.GroupVersionKind().Kind,
			Name:      healthCheck.Name,
			Namespace: healthCheck.Namespace,
		},
		Spec: &extpb.DNSHealthCheckSpec{
			Port:                     int64(healthCheck.Spec.Port),
			Hostname:                 healthCheck.Spec.Hostname,
			Address:                  healthCheck.Spec.Address,
			Path:                     healthCheck.Spec.Path,
			Protocol:                 string(healthCheck.Spec.Protocol),
			Interval:                 healthCheck.Spec.Interval.String(),
			AdditionalHeadersRef:     additionalHeadersRef,
			FailureThreshold:         int64(healthCheck.Spec.FailureThreshold),
			AllowInsecureCertificate: healthCheck.Spec.AllowInsecureCertificate,
		},
		Status: &extpb.DNSHealthCheckStatus{
			LastCheckAt:         healthCheck.Status.LastCheckedAt.Unix(),
			ConsecutiveFailures: int64(healthCheck.Status.ConsecutiveFailures),
			Reason:              healthCheck.Status.Reason,
			Status:              int64(healthCheck.Status.Status),
			Healthy:             healthy,
			ObservedGeneration:  healthCheck.Status.ObservedGeneration,
		},
	}
}
