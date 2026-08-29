# Run the kuma microbenchmarks on a Windows host.
#
# scripts/micro.sh copies this over and runs it. It is the same work the Linux
# branch of that script does inline, written out here because quoting a shell
# script through ssh into cmd.exe and then into PowerShell is not something
# anybody should have to read.
#
# With -Facts it prints the commit, the Go version and the memory size on one
# line instead of running anything, which is what the caller needs to fill in
# the parts of a record the test binary cannot know.

param(
	[string]$Ref = "origin/main",
	[int]$Count = 5,
	[string]$Bench = ".",
	[string]$Packages = "./...",
	[string]$Dir = ".",
	[string]$GoExperiment = "",
	[switch]$Facts
)

$ErrorActionPreference = "Stop"

$go = Join-Path $env:USERPROFILE "sdk\go1.27.0\bin\go.exe"
$src = Join-Path $env:USERPROFILE "src\kuma"

if ($Facts) {
	Push-Location $src
	$commit = (& git rev-parse HEAD).Trim()
	Pop-Location
	$goVersion = (& $go env GOVERSION).Trim()
	$memoryGB = [int][math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory / 1GB)
	Write-Output "$commit $goVersion $memoryGB"
	exit 0
}

New-Item -ItemType Directory -Force -Path (Split-Path $src) | Out-Null
if (-not (Test-Path (Join-Path $src ".git"))) {
	& git clone -q https://github.com/tamnd/kuma.git $src
}

Set-Location $src
& git fetch -q origin

# A bare branch name is resolved against origin first, for the same reason the
# shell script does it: git wants to make a local branch out of a name it only
# knows as a remote one, and --detach will not sit next to that.
$at = (& git rev-parse -q --verify "origin/$Ref^{commit}")
if (-not $at) {
	$at = (& git rev-parse -q --verify "$Ref^{commit}")
}
if (-not $at) {
	# Saying so here is the difference between one clear line and a page of
	# PowerShell about calling a method on a null. A branch that was merged and
	# deleted while the run was going is how this happens in practice.
	Write-Error "micro: $Ref is not a commit on this host, so there is nothing to measure"
	exit 1
}
& git checkout -q --detach $at.Trim()

if ($GoExperiment -ne "") {
	$env:GOEXPERIMENT = $GoExperiment
}

& $go version

# kuma is more than one module, and the nested one is measured by running in its
# directory rather than by naming it as a package, since the root ./... does not
# reach across a go.mod.
Set-Location (Join-Path $src $Dir)

# A list of packages arrives as one string, because ssh and PowerShell between
# them have already decided that a quoted argument is one argument. Splitting it
# here is what turns it back into the several arguments go test wants.
#
# The @() around it is not decoration. A pipeline that produces one item hands
# back that item rather than a list holding it, and splatting a string spreads
# its characters, so a run of one package asked go test for a package called "."
# and then one called "k" and then one called "e". Forcing the list keeps a list
# of one a list.
$pkgs = @($Packages -split '\s+' | Where-Object { $_ -ne "" })
& $go test -run "^$" -bench $Bench -benchmem -count $Count @pkgs
