package main

import (
	"path"
	"runtime"

	"go.skia.org/infra/go/auth"
	"go.skia.org/infra/go/gce"
	"go.skia.org/infra/go/gce/server"
)

func CtBase(name, ipAddress string) *gce.Instance {
	vm := server.Server20170613(name)
	vm.ExternalIpAddress = ipAddress
	vm.Metadata["owner_primary"] = "rmistry"
	vm.Metadata["owner_secondary"] = "benjaminwagner"
	return vm
}

func CTFE() *gce.Instance {
	vm := CtBase("skia-ctfe", "104.154.112.110")
	vm.DataDisk = nil
	vm.MachineType = gce.MACHINE_TYPE_STANDARD_2
	return vm
}

func CtMaster() *gce.Instance {
	vm := CtBase("skia-ct-master", "104.154.112.17")
	vm.DataDisk.SizeGb = 500
	vm.DataDisk.Type = gce.DISK_TYPE_PERSISTENT_STANDARD
	vm.MachineType = gce.MACHINE_TYPE_HIGHMEM_16
	vm.Scopes = append(vm.Scopes, auth.SCOPE_GERRIT)

	_, filename, _, _ := runtime.Caller(0)
	dir := path.Dir(filename)
	vm.SetupScript = path.Join(dir, "setup-script-master.sh")
	return vm
}

func main() {
	server.Main(gce.ZONE_DEFAULT, map[string]*gce.Instance{
		"ctfe":   CTFE(),
		"master": CtMaster(),
	})
}
