package cel

import (
	"fmt"
	"log"
	"reflect"
	"strings"

	"github.com/golang/protobuf/jsonpb"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"
	v1 "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
	"github.com/tidwall/gjson"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/structpb"
)

const RootMetadataBinding = "metadata"
const RootRequestBinding = "request"
const RootSourceBinding = "source"
const RootDestinationBinding = "destination"
const RootAuthBinding = "auth"

type Predicate struct {
	program cel.Program
	source  string
}

func NewPredicate(source string) (*Predicate, error) {
	program, err := Compile(source, cel.BoolType)
	if err != nil {
		return nil, err
	}
	return &Predicate{
		program: program,
		source:  source,
	}, nil
}

func (p *Predicate) Matches(json string) (bool, error) {
	input, err := AuthJsonToCel(json)
	if err != nil {
		return false, err
	}
	result, _, err := p.program.Eval(input)
	if err != nil {
		return false, err
	}
	return result.Value().(bool), nil
}

type Expression struct {
	program cel.Program
	source  string
}

type StringExpression struct {
	expression Expression
}

func NewExpression(source string) (*Expression, error) {
	program, err := Compile(source, nil)
	if err != nil {
		return nil, err
	}
	return &Expression{
		program: program,
		source:  source,
	}, nil
}

func (e *Expression) ResolveFor(json string) (interface{}, error) {
	result, _, err := e.Evaluate(json)
	if err != nil {
		return nil, err
	}

	// this is for backwards compatibility with JSONValue, these should interoperate seamlessly this way
	if jsonLiteral, err := ValueToJSON(result); err != nil {
		return nil, err
	} else {
		return gjson.Parse(jsonLiteral).Value(), nil
	}
}

func (e *Expression) Evaluate(json string) (ref.Val, *cel.EvalDetails, error) {
	input, err := AuthJsonToCel(json)
	if err != nil {
		return nil, nil, err
	}

	return e.program.Eval(input)
}

func Compile(expression string, expectedType *cel.Type, opts ...cel.EnvOption) (cel.Program, error) {

	registry, _ := types.NewRegistry(
		&v1.Metadata{},
		&v1.TargetRef{},
		&v1.Condition{},
		&v1.ConditionStatus{},

		&v1.Policy{},
		&v1.PolicyStatus{},
		&v1.Gateway{},
		&v1.DNSHealthCheckProbe{},
		&v1.DNSHealthCheckStatus{},
		&v1.DNSRecord{},
	)

	envOpts := append([]cel.EnvOption{}, opts...)
	envOpts = append(envOpts, cel.CustomTypeAdapter(registry))
	envOpts = append(envOpts, cel.CustomTypeProvider(registry))
	envOpts = append(envOpts, cel.Variable("policy", cel.ObjectType("kuadrant.v1.Policy")))
	envOpts = append(envOpts, cel.Variable("gateway", cel.ObjectType("kuadrant.v1.Gateway")))
	envOpts = append(envOpts, cel.Variable("dnsRecord", cel.ObjectType("kuadrant.v1.DNSRecord")))
	envOpts = append(envOpts, cel.Variable("healthCheck", cel.ObjectType("kuadrant.v1.DNSHealthCheckProbe")))

	envOpts = append(envOpts, hasHealthcheckCELFunc())
	envOpts = append(envOpts, healthCheckUnhealthyCELFunc())
	envOpts = append(envOpts, ext.Strings())
	env, env_err := cel.NewEnv(envOpts...)
	if env_err != nil {
		return nil, env_err
	}

	ast, issues := env.Parse(expression)
	if issues.Err() != nil {
		return nil, issues.Err()
	}

	checked, issues := env.Check(ast)
	if issues.Err() != nil {
		return nil, issues.Err()
	}

	if expectedType != nil {
		if !reflect.DeepEqual(checked.OutputType(), expectedType) && !reflect.DeepEqual(checked.OutputType(), cel.DynType) {
			return nil, fmt.Errorf("type error: got %v, wanted %v output type", checked.OutputType(), expectedType)
		}
	}

	program, err := env.Program(checked)
	if err != nil {
		return nil, err
	}
	return program, nil
}

func refToProto[T protoreflect.ProtoMessage](val ref.Val) (t T, err error) {
	value, err := cel.RefValueToValue(val)
	if err != nil {
		return t, err
	}
	v, err := value.GetObjectValue().UnmarshalNew()
	if err != nil {
		return t, err
	}
	return v.(T), nil
}

func hasListenersCELFunc() cel.EnvOption {
	return cel.Function("hasNoListeners",
		cel.MemberOverload("gateway_has_no_listeners",
			[]*cel.Type{
				cel.ObjectType("kuadrant.v1.Gateway"),
			},
			cel.BoolType,
			cel.UnaryBinding(func(value ref.Val) ref.Val {
				gateway, err := refToProto[*v1.Gateway](value)
				if err != nil {
					return types.NewErr("pbError: %w", err)
				}

				if len(gateway.Status.Listeners) > 0 {
					return types.False
				}

				return types.True

			}),
		),
	)
}

func hasHealthcheckCELFunc() cel.EnvOption {
	return cel.Function("hasOtherOwners",
		cel.MemberOverload("dnsrecord_has_other_owners",
			[]*cel.Type{
				cel.ObjectType("kuadrant.v1.DNSRecord"),
			},
			cel.BoolType,
			cel.UnaryBinding(func(value ref.Val) ref.Val {
				dnsrecord, err := refToProto[*v1.DNSRecord](value)
				if err != nil {
					return types.NewErr("pbError: %w", err)
				}

				otherOwners := []string{}
				for _, owner := range dnsrecord.Status.DomainOwners {
					if owner != dnsrecord.Status.OwnerID {
						otherOwners = append(otherOwners, owner)
					}
				}
				if len(otherOwners) > 1 {
					return types.True
				}

				return types.False

			}),
		),
	)
}

func healthCheckUnhealthyCELFunc() cel.EnvOption {
	return cel.Function("isUnhealthy",
		cel.MemberOverload("healthcheck_is_unhealthy",
			[]*cel.Type{
				cel.ObjectType("kuadrant.v1.DNSHealthCheckProbe"),
			},
			cel.BoolType,
			cel.UnaryBinding(func(value ref.Val) ref.Val {
				probe, err := refToProto[*v1.DNSHealthCheckProbe](value)
				if err != nil {
					return types.NewErr("pbError: %w", err)
				}

				if probe.Spec.Address == "" {
					//health check doesn't exist return false
					return types.False
				}

				log.Printf("got probe %+s", probe)
				if probe.Status.Healthy {
					return types.False
				}

				return types.True

			}),
		),
	)
}

func ValueToJSON(val ref.Val) (string, error) {
	v, err := val.ConvertToNative(reflect.TypeOf(&structpb.Value{}))
	if err != nil {
		return "", err
	}
	marshaller := protojson.MarshalOptions{Multiline: false}
	bytes, err := marshaller.Marshal(v.(proto.Message))
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// todo this should eventually be sourced as proper proto from the pipeline
func AuthJsonToCel(json string) (map[string]interface{}, error) {
	data := structpb.Struct{}
	if err := jsonpb.Unmarshal(strings.NewReader(json), &data); err != nil {
		return nil, err
	}
	metadata := data.GetFields()[RootMetadataBinding]
	request := data.GetFields()[RootRequestBinding]
	source := data.GetFields()[RootSourceBinding]
	destination := data.GetFields()[RootDestinationBinding]
	auth := data.GetFields()[RootAuthBinding]

	input := map[string]interface{}{
		RootMetadataBinding:    metadata,
		RootRequestBinding:     request,
		RootSourceBinding:      source,
		RootDestinationBinding: destination,
		RootAuthBinding:        auth,
	}
	return input, nil
}
