package modulescope

import "modulescope_dep"

func fieldInSiblingPackageStillReported(d *modulescope_dep.Dep) int {
	return d.Field.N // want "d\\.Field may be nil here"
}
