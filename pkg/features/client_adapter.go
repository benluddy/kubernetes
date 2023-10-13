/*
Copyright 2024 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package features

import (
	"fmt"

	"k8s.io/component-base/featuregate"
)

// TODO
type clientAdapter[N ~string, S ~struct {
	Default       bool
	LockToDefault bool
	PreRelease    P
}, P ~string] struct {
	mfg                             featuregate.MutableFeatureGate
	palpha, pbeta, pga, pdeprecated P
}

func newClientAdapter[N ~string, S ~struct {
	Default       bool
	LockToDefault bool
	PreRelease    P
}, P ~string](mfg featuregate.MutableFeatureGate, alpha, beta, ga, deprecated P) clientAdapter[N, S, P] {
	return clientAdapter[N, S, P]{
		mfg:         mfg,
		palpha:      alpha,
		pbeta:       beta,
		pga:         ga,
		pdeprecated: deprecated,
	}
}

// Trying to instantiate an adapter "from" the component-base types themselves will refuse to
// compile if the component-base types no longer satisfy the adapter's type constraints. This covers
// changes like the addition of a new field to FeatureSpec, which would require a corresponding
// change to both client-go's FeatureSpec and to the adapter.
var _ = newClientAdapter[featuregate.Feature, featuregate.FeatureSpec](
	nil,
	featuregate.Alpha, featuregate.Beta, featuregate.GA, featuregate.Deprecated,
)

func (a clientAdapter[N, _, _]) Enabled(name N) bool {
	return a.mfg.Enabled(featuregate.Feature(name))
}

func (a clientAdapter[N, S, P]) Add(in map[N]S) error {
	out := map[featuregate.Feature]featuregate.FeatureSpec{}
	for name, spec := range in {
		underlying := struct {
			Default       bool
			LockToDefault bool
			PreRelease    P
		}(spec)
		converted := featuregate.FeatureSpec{
			Default:       underlying.Default,
			LockToDefault: underlying.LockToDefault,
		}
		switch underlying.PreRelease {
		case a.palpha:
			converted.PreRelease = featuregate.Alpha
		case a.pbeta:
			converted.PreRelease = featuregate.Beta
		case a.pga:
			converted.PreRelease = featuregate.GA
		case a.pdeprecated:
			converted.PreRelease = featuregate.Deprecated
		default:
			// The default case implies programmer error.  The same set of prerelease
			// constants must exist in both component-base and client-go, and each one
			// must have a case here.
			panic(fmt.Sprintf("unrecognized prerelease %q of feature %q", underlying.PreRelease, name))
		}
		out[featuregate.Feature(name)] = converted
	}
	return a.mfg.Add(out)
}
