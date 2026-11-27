# Prepara lo que Terraform NO crea: los secretos (con valores que no se versionan) y las imagenes.
# Orden de uso (ver infra/terraform/README.md):
#   1. terraform apply -target=... (APIs, repositorio de imagenes, cuenta de servicio del backend)
#   2. .\scripts\terraform\preparar.ps1          <- este script
#   3. terraform apply -var="image_tag=<tag>"
# Requiere gcloud autenticado (gcloud auth login) y Docker no hace falta: las imagenes se construyen en Cloud Build.
param(
    [string]$Project     = "proyecto-plataforma-mooc",
    [string]$Region      = "us-central1",
    [string]$RepoName    = "mooc-repo",
    [string]$SqlInstance = "mooc-db-instance",
    [string]$DbUser      = "mooc_user",
    [string]$DbName      = "mooc_db",
    [string]$Suffix      = "-tf",
    [string]$Tag         = "",
    [switch]$SkipBuild
)

$ErrorActionPreference = "Stop"

function Write-Step([string]$Text) { Write-Host ""; Write-Host "==> $Text" -ForegroundColor Cyan }
function Write-Ok([string]$Text)   { Write-Host "    $Text" -ForegroundColor Green }

function Test-Secret([string]$Name) {
    $out = gcloud secrets list --project=$Project --filter="name:$Name" --format="value(name)"
    # gcloud puede devolver el nombre corto (mooc-db-url) o la ruta completa (projects/N/secrets/mooc-db-url).
    $exact = @($out -split "`r?`n" | Where-Object { $_.Trim() -eq $Name -or $_.Trim().EndsWith("/secrets/$Name") })
    return $exact.Count -gt 0
}

# Crea un secreto con el valor EXACTO (sin salto de linea al final) usando un archivo temporal.
function New-Secret([string]$Name, [string]$Value) {
    $tmp = [System.IO.Path]::GetTempFileName()
    try {
        [System.IO.File]::WriteAllText($tmp, $Value)
        gcloud secrets create $Name --project=$Project --replication-policy=automatic --data-file=$tmp --quiet
        if ($LASTEXITCODE -ne 0) { throw "No se pudo crear el secreto $Name" }
    } finally {
        Remove-Item $tmp -Force -ErrorAction SilentlyContinue
    }
}

if (-not $Tag) {
    $Tag = (git rev-parse --short HEAD).Trim()
}
$ProjectNumber = (gcloud projects describe $Project --format="value(projectNumber)").Trim()
$RepoUrl = "$Region-docker.pkg.dev/$Project/$RepoName"
$BackendSa = "sa-mooc-backend$Suffix@$Project.iam.gserviceaccount.com"

# ---------------- Secretos ----------------
Write-Step "1. Secretos en Secret Manager (los que ya existen no se tocan)"

if (-not (Test-Secret "mooc-db-url")) {
    Write-Host "    Ingresa la contrasena del usuario $DbUser de Cloud SQL (no se muestra ni se guarda en el repositorio):"
    $secure = Read-Host -AsSecureString
    $plain = [System.Net.NetworkCredential]::new("", $secure).Password
    if ($plain.Length -lt 8) { throw "La contrasena es demasiado corta." }
    $enc = [System.Uri]::EscapeDataString($plain)
    $dbUrl = "postgres://${DbUser}:${enc}@/${DbName}?host=/cloudsql/${Project}:${Region}:${SqlInstance}&sslmode=disable"
    New-Secret "mooc-db-url" $dbUrl
    Write-Ok "mooc-db-url creado."
} else { Write-Ok "mooc-db-url ya existe." }

if (-not (Test-Secret "mooc-jwt-secret")) {
    $bytes = New-Object byte[] 48
    [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
    New-Secret "mooc-jwt-secret" ([Convert]::ToBase64String($bytes))
    Write-Ok "mooc-jwt-secret creado con un valor aleatorio."
} else { Write-Ok "mooc-jwt-secret ya existe." }

$hasAccess = Test-Secret "mooc-storage-access-key"
$hasSecret = Test-Secret "mooc-storage-secret-key"
if (-not ($hasAccess -and $hasSecret)) {
    Write-Host "    Generando la clave HMAC de Cloud Storage para $BackendSa ..."
    $hmac = gcloud storage hmac create $BackendSa --project=$Project --format=json | ConvertFrom-Json
    $accessId = $hmac.metadata.accessId
    if (-not $accessId) { $accessId = $hmac.accessId }
    if (-not $accessId -or -not $hmac.secret) { throw "No se pudo generar la clave HMAC. Revisa que exista la cuenta $BackendSa." }
    if (-not $hasAccess) { New-Secret "mooc-storage-access-key" $accessId }
    if (-not $hasSecret) { New-Secret "mooc-storage-secret-key" $hmac.secret }
    Write-Ok "Secretos HMAC creados."
} else { Write-Ok "Secretos HMAC ya existen." }

# ---------------- Imagenes ----------------
if ($SkipBuild) {
    Write-Step "2. Imagenes omitidas (-SkipBuild)"
} else {
    Write-Step "2. Construyendo imagenes en Cloud Build (tag: $Tag)"
    gcloud builds submit --project=$Project --config=cloudbuild.yaml --substitutions="_REPO_URL=$RepoUrl,_TAG=$Tag" .
    if ($LASTEXITCODE -ne 0) { throw "Fallo la construccion de backend y worker." }

    # La URL del backend es deterministica, asi que se conoce antes de desplegarlo.
    $BackendUrl = "https://mooc-backend$Suffix-$ProjectNumber.$Region.run.app"
    gcloud builds submit --project=$Project --config=cloudbuild-frontend.yaml --substitutions="_REPO_URL=$RepoUrl,_TAG=$Tag,_BACKEND_URL=$BackendUrl" .
    if ($LASTEXITCODE -ne 0) { throw "Fallo la construccion del frontend." }
    Write-Ok "Imagenes publicadas en $RepoUrl con el tag $Tag."
}

Write-Host ""
Write-Host "Listo. Ahora, desde infra\terraform:" -ForegroundColor Green
Write-Host "  terraform plan `"-var=image_tag=$Tag`" -out=tfplan"
Write-Host "  terraform apply tfplan"
