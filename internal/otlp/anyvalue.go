package otlp

import (
	"encoding/base64"
	"math"
	"strconv"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
)

// value turns an OTLP AnyValue into the value encoding/json writes for it:
// strings, bools, int64s, and doubles as themselves, bytes as base64,
// arrays as slices, and kvlists as objects. Doubles JSON cannot hold (NaN
// and infinities) become their string form. An unset value is nil.
func value(v *commonpb.AnyValue) any {
	switch x := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return x.StringValue
	case *commonpb.AnyValue_BoolValue:
		return x.BoolValue
	case *commonpb.AnyValue_IntValue:
		return x.IntValue
	case *commonpb.AnyValue_DoubleValue:
		if math.IsNaN(x.DoubleValue) || math.IsInf(x.DoubleValue, 0) {
			return strconv.FormatFloat(x.DoubleValue, 'g', -1, 64)
		}
		return x.DoubleValue
	case *commonpb.AnyValue_BytesValue:
		return base64.StdEncoding.EncodeToString(x.BytesValue)
	case *commonpb.AnyValue_ArrayValue:
		values := x.ArrayValue.GetValues()
		out := make([]any, len(values))
		for i, e := range values {
			out[i] = value(e)
		}
		return out
	case *commonpb.AnyValue_KvlistValue:
		return object(x.KvlistValue.GetValues())
	default:
		return nil
	}
}

// object turns key-value pairs into a JSON object. A repeated key keeps its
// last value.
func object(kvs []*commonpb.KeyValue) map[string]any {
	out := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		out[kv.GetKey()] = value(kv.GetValue())
	}
	return out
}

// stringAttr returns the string value of key in kvs, or "".
func stringAttr(kvs []*commonpb.KeyValue, key string) string {
	var s string
	for _, kv := range kvs {
		if kv.GetKey() == key {
			s = kv.GetValue().GetStringValue()
		}
	}
	return s
}
