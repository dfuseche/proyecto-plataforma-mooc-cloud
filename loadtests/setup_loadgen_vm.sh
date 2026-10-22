#!/usr/bin/env bash
#
# Script de aprovisionamiento y optimización para la VM Generadora de Carga en GCP.
# Ejecutar en Ubuntu 22.04 LTS / Debian 12 con privilegios sudo:
#   chmod +x setup_loadgen_vm.sh && sudo ./setup_loadgen_vm.sh
#

set -euo pipefail

echo "=== [1/4] Optimizando límites del sistema y stack de red (TCP/Sockets) ==="

# Aumentar límites de descriptores de archivos para k6
cat << 'EOF' | tee /etc/security/limits.d/99-k6.conf
* soft nofile 65536
* hard nofile 65536
* soft nproc 65536
* hard nproc 65536
root soft nofile 65536
root hard nofile 65536
EOF

# Parámetros del kernel para alto volumen de conexiones concurrentes
cat << 'EOF' | tee /etc/sysctl.d/99-k6.conf
fs.file-max = 2097152
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 15
net.core.somaxconn = 65535
net.core.netdev_max_backlog = 10000
net.ipv4.tcp_max_syn_backlog = 8192
EOF

sysctl --system >/dev/null

echo "=== [2/4] Instalando dependencias base (curl, git, jq, psql) ==="
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y ca-certificates curl gnupg git jq postgresql-client-common postgresql-client

echo "=== [3/4] Instalando k6 oficial ==="
if ! command -v k6 >/dev/null 2>&1; then
  mkdir -p -m 755 /etc/apt/keyrings
  curl -fsSL https://dl.k6.io/key.gpg | gpg --dearmor -o /etc/apt/keyrings/k6.gpg --yes
  chmod a+r /etc/apt/keyrings/k6.gpg
  echo "deb [signed-by=/etc/apt/keyrings/k6.gpg] https://dl.k6.io/deb stable main" | tee /etc/apt/sources.list.d/k6.list
  apt-get update -y
  apt-get install -y k6
fi

echo "=== [4/4] Verificando versiones y límites ==="
echo "k6 versión: $(k6 version)"
echo "Límite nofile actual: $(ulimit -n)"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
chmod +x "${SCRIPT_DIR}/k6/"*.sh 2>/dev/null || true

echo ""
echo "=========================================================================="
echo " VM Generadora de Carga aprovisionada exitosamente."
echo ""
echo " Para ejecutar pruebas de carga:"
echo "   1. Escenario 1 (Actividad Académica Concurrente):"
echo "      cd ${SCRIPT_DIR}/k6"
echo "      ./run_escenario1_niveles.sh -b \"https://<IP_LOAD_BALANCER>\""
echo ""
echo "   2. Escenario 2 (Carga, Procesamiento y Consumo Multimedia HLS):"
echo "      cd ${SCRIPT_DIR}/k6"
echo "      ./run_escenario2_niveles.sh -b \"https://<IP_LOAD_BALANCER>\""
echo ""
echo " Durante la ejecución, el dashboard web de k6 estará en:"
echo "   http://<IP_PUBLICA_DE_ESTA_VM>:5665"
echo "=========================================================================="
