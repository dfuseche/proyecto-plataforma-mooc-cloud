# Crea una version nueva de la plantilla de instancia de cada MIG con el
# startup-script actual del repo (deploy/startup/startup.sh), la asigna al MIG
# y reemplaza las instancias sin bajar capacidad (max-surge 1, max-unavailable 0).
#
# Las plantillas son inmutables y guardan una COPIA del startup-script: cambiar
# startup.sh en el repo no afecta a los MIG hasta correr este script. La copia
# conserva todo lo demas de la plantilla actual (tipo de maquina, disco, red,
# cuenta de servicio, metadatos mooc-role/mooc-secret/mooc-image).
#
# Uso:  .\scripts\gcp\actualizar-plantillas.ps1 [-Grupos api,worker] [-Image <imagen:tag>] [-SinReemplazo]
#   -Grupos        grupos a actualizar (por defecto los dos)
#   -Image         ademas fija el metadato mooc-image (p. ej. ...:<sha> en vez de :latest)
#   -SinReemplazo  solo crea y asigna la plantilla; las VMs nuevas la usaran al crearse
# Al final imprime la plantilla anterior de cada grupo, por si hay que volver atras.

param(
    [ValidateSet('api', 'worker')][string[]]$Grupos = @('api', 'worker'),
    [string]$Image,
    [switch]$SinReemplazo
)

. "$PSScriptRoot\config.ps1"
Initialize-Gcp

# Windows PowerShell 5.1 puede serializar arreglos como {"value":[...],"Count":n}
# por un TypeData de System.Array; quitarlo deja el JSON con arreglos normales.
Remove-TypeData -TypeName System.Array -ErrorAction SilentlyContinue

$startupPath = Join-Path $PSScriptRoot '..\..\deploy\startup\startup.sh'
$startup = [IO.File]::ReadAllText((Resolve-Path $startupPath)) -replace "`r`n", "`n"

function Set-MetadataValue($Properties, [string]$Key, [string]$Value) {
    if (-not $Properties.metadata) {
        $Properties | Add-Member -NotePropertyName metadata -NotePropertyValue ([pscustomobject]@{}) -Force
    }
    $items = @($Properties.metadata.items | Where-Object { $_ })
    $item = $items | Where-Object { $_.key -eq $Key } | Select-Object -First 1
    if ($item) {
        $item.value = $Value
    } else {
        $items += [pscustomobject]@{ key = $Key; value = $Value }
    }
    $Properties.metadata | Add-Member -NotePropertyName items -NotePropertyValue $items -Force
}

# Quita campos de solo salida (kind, fingerprint) que la API no espera al crear.
function Remove-OutputOnly($Node) {
    if ($Node -is [System.Management.Automation.PSCustomObject]) {
        foreach ($name in @($Node.PSObject.Properties.Name)) {
            if ($name -in @('kind', 'fingerprint')) { $Node.PSObject.Properties.Remove($name) }
            else { Remove-OutputOnly $Node.$name }
        }
    } elseif ($Node -is [System.Array]) {
        foreach ($child in $Node) { Remove-OutputOnly $child }
    }
}

function Wait-GcpOperation($Operation) {
    $limite = (Get-Date).AddMinutes(5)
    while ($Operation.status -ne 'DONE') {
        if ((Get-Date) -gt $limite) { throw "La operacion $($Operation.name) no termino en 5 minutos." }
        Start-Sleep -Seconds 2
        $Operation = Get-GcpJson $Operation.selfLink
    }
    if ($Operation.error) { throw "La operacion fallo: $($Operation.error.errors | ConvertTo-Json -Depth 5 -Compress)" }
}

$stamp = (Get-Date).ToUniversalTime().ToString('yyyyMMdd-HHmm')
$anteriores = [ordered]@{}

foreach ($grupo in $Grupos) {
    $mig = if ($grupo -eq 'api') { $ApiMig } else { $WorkerMig }
    Write-Step "$mig"

    $url = Get-MigTemplateUrl $mig
    $tpl = Get-GcpJson $url
    $props = $tpl.properties
    $anteriores[$mig] = $url
    Write-Host "    plantilla actual: $($tpl.name)"

    $sa = @($props.serviceAccounts)[0]
    if ($sa) { Write-Host "    cuenta de servicio: $($sa.email)  scopes: $(@($sa.scopes) -join ', ')" }

    $role = Get-MetadataValue $props 'mooc-role'
    if (-not $role) { throw "La plantilla $($tpl.name) no tiene el metadato mooc-role: no parece una plantilla de este proyecto." }

    $mismoScript = (Get-MetadataValue $props 'startup-script') -replace "`r`n", "`n"
    if ($mismoScript -eq $startup -and (-not $Image -or (Get-MetadataValue $props 'mooc-image') -eq $Image)) {
        Write-Ok 'Ya tiene el startup-script actual; no se crea otra plantilla.'
        continue
    }

    Set-MetadataValue $props 'startup-script' $startup
    if ($Image) { Set-MetadataValue $props 'mooc-image' $Image }
    Remove-OutputOnly $props

    $base = ($tpl.name -replace '-\d{8}-\d{4}$', '')
    if ($base.Length -gt 49) { $base = $base.Substring(0, 49).TrimEnd('-') }
    $nuevo = "$base-$stamp"
    $body = [ordered]@{
        name        = $nuevo
        description = "Copia de $($tpl.name) con deploy/startup/startup.sh actualizado (actualizar-plantillas.ps1)"
        properties  = $props
    } | ConvertTo-Json -Depth 50

    # Coleccion = URL de la plantilla sin el nombre (sirve para plantillas globales y regionales).
    $coleccion = $url.Substring(0, $url.LastIndexOf('/'))
    try {
        $op = Invoke-RestMethod -Method Post -Uri $coleccion -Headers @{ Authorization = "Bearer $(Get-AccessToken)" } `
            -ContentType 'application/json; charset=utf-8' -Body ([Text.Encoding]::UTF8.GetBytes($body))
    } catch {
        throw "No se pudo crear la plantilla ${nuevo}: $($_.ErrorDetails.Message)"
    }
    Wait-GcpOperation $op
    Write-Ok "Plantilla creada: $nuevo"

    # Plantilla global: basta el nombre. Regional: se pasa la URL completa.
    $templateArg = if ($coleccion -match '/global/') { $nuevo } else { "$coleccion/$nuevo" }
    Invoke-G @('compute', 'instance-groups', 'managed', 'set-instance-template', $mig, "--template=$templateArg",
        "--project=$Project", "--zone=$script:Zone")
    Write-Ok "$mig usa ahora $nuevo"

    if (-not $SinReemplazo) {
        Write-Host '    Reemplazando instancias (crea la nueva antes de borrar la vieja)...'
        Invoke-G @('compute', 'instance-groups', 'managed', 'rolling-action', 'replace', $mig,
            "--project=$Project", "--zone=$script:Zone", '--max-surge=1', '--max-unavailable=0')
        Invoke-G @('compute', 'instance-groups', 'managed', 'wait-until', $mig, '--stable',
            "--project=$Project", "--zone=$script:Zone", '--timeout=900')
        Write-Ok "$mig estable con la plantilla nueva."
    }
}

Write-Host ""
Write-Host 'Para volver atras, por grupo:' -ForegroundColor Cyan
foreach ($mig in $anteriores.Keys) {
    Write-Host "    gcloud compute instance-groups managed set-instance-template $mig --template=$($anteriores[$mig]) --zone=$script:Zone --project=$Project"
    Write-Host "    gcloud compute instance-groups managed rolling-action replace $mig --max-surge=1 --max-unavailable=0 --zone=$script:Zone --project=$Project"
}
