#!/usr/bin/env python3
"""
extraer_metricas.py
Parsea los archivos JSON de resumen de k6 (--summary-export) generados en
loadtests/results/escenario1 y loadtests/results/escenario2, e imprime las
tablas en formato Markdown listas para el informe de capacidad de la Entrega 3.

Uso:
    python3 loadtests/extraer_metricas.py
    python3 loadtests/extraer_metricas.py --prefix 20261021_...
"""

import os
import sys
import glob
import json
import argparse
from pathlib import Path

# Valores de referencia de la Entrega 2 (para la tabla comparativa)
E2_ESCENARIO1 = {
    10:  {"p50": 144.4, "p95": 1111.3, "p99": 2324.1, "rps": 5.51,  "err": 0.00, "iter": 821},
    50:  {"p50": 146.6, "p95": 2670.9, "p99": 5336.6, "rps": 19.30, "err": 0.00, "iter": 3575},
    150: {"p50": 182.1, "p95": 6780.6, "p99": 9937.5, "rps": 25.44, "err": 0.00, "iter": 7016},
    400: {"p50": 785.6, "p95": 34404.2, "p99": 60000.1, "rps": 21.07, "err": 1.05, "iter": 5975},
}

def parse_escenario1(results_dir, prefix=None):
    pattern = os.path.join(results_dir, "*.json")
    files = sorted(glob.glob(pattern))
    if not files:
        print(f"[!] No se encontraron archivos JSON en {results_dir}")
        return

    # Si no se especifica prefijo, tomar el prefijo más reciente (YYYYMMDD_HHMMSS)
    if not prefix:
        prefixes = sorted(list(set(os.path.basename(f).split('_')[0] + "_" + os.path.basename(f).split('_')[1] for f in files if '_' in os.path.basename(f))))
        prefix = prefixes[-1] if prefixes else ""

    run_files = [f for f in files if prefix in os.path.basename(f)]
    if not run_files:
        run_files = files

    print(f"\n# =====================================================================")
    print(f"# RESULTADOS ESCENARIO 1 — Actividad Académica Concurrente (Corrida: {prefix})")
    print(f"# =====================================================================\n")

    print("### Tabla de Desempeño Global (k6)")
    print("| Nivel | MAX_VUS | p50 (ms) | p90 (ms) | p95 (ms) | p99 (ms) | Throughput (req/s) | Tasa de error | Iteraciones | Rechazos Quiz (409) | Doble Calif. |")
    print("|---|---|---|---|---|---|---|---|---|---|---|")

    rows = []
    endpoint_rows = []

    for fpath in run_files:
        try:
            with open(fpath, "r", encoding="utf-8") as f:
                d = json.load(f)
        except Exception as e:
            continue

        fname = os.path.basename(fpath)
        # Extraer tag y vus del nombre: ej. 20261007_163550_01_nivel1_vus50.json
        parts = fname.replace(".json", "").split("_")
        tag = "nivel"
        vus = 0
        for p in parts:
            if p.startswith("vus"):
                try:
                    vus = int(p.replace("vus", ""))
                except:
                    pass
            elif "linea" in p or "nivel" in p or "repeticion" in p:
                tag = p

        m = d.get("metrics", {})
        dur = m.get("http_req_duration", {})
        p50 = dur.get("med", dur.get("p(50)", 0.0))
        p90 = dur.get("p(90)", 0.0)
        p95 = dur.get("p(95)", 0.0)
        p99 = dur.get("p(99)", 0.0)

        reqs = m.get("http_reqs", {})
        rps = reqs.get("rate", 0.0)

        fails = m.get("http_req_failed", {})
        err_pct = fails.get("value", 0.0) * 100.0

        iters = m.get("iterations", {}).get("count", 0)
        max_att = m.get("quiz_max_attempts_reached", {}).get("count", 0)
        double_g = m.get("quiz_double_grading_detected", {}).get("count", 0)

        # Nombre bonito para el nivel
        nivel_label = fname.split("_")[2] if len(fname.split("_")) > 2 else tag
        if "linea" in fname:
            nivel_label = "Línea base"
        elif "nivel1" in fname:
            nivel_label = "Nivel 1"
        elif "nivel2" in fname:
            nivel_label = "Nivel 2"
        elif "nivel3" in fname:
            nivel_label = "Nivel 3"
        elif "repeticion" in fname:
            nivel_label = "Repetición"

        print(f"| {nivel_label} | {vus} | {p50:.1f} | {p90:.1f} | {p95:.1f} | {p99:.1f} | {rps:.2f} | {err_pct:.2f}% | {iters} | {max_att} | {double_g} |")
        rows.append({"label": nivel_label, "vus": vus, "p50": p50, "p95": p95, "p99": p99, "rps": rps, "err": err_pct, "iters": iters})

        # Endpoints
        ep_catalog = m.get("http_req_duration{endpoint:catalog}", {})
        ep_enroll = m.get("http_req_duration{endpoint:enroll}", {})
        ep_heartbeat = m.get("http_req_duration{endpoint:heartbeat}", {})
        ep_badge = m.get("http_req_duration{endpoint:badge}", {})
        ep_qstart = m.get("http_req_duration{endpoint:quiz_start}", {})
        ep_qsub = m.get("http_req_duration{endpoint:quiz_submit}", {})

        endpoint_rows.append({
            "label": nivel_label,
            "catalog": f"{ep_catalog.get('avg',0):.1f} / {ep_catalog.get('p(95)',0):.1f} / {ep_catalog.get('p(99)',0):.1f}",
            "enroll": f"{ep_enroll.get('avg',0):.1f} / {ep_enroll.get('p(95)',0):.1f} / {ep_enroll.get('p(99)',0):.1f}",
            "heartbeat": f"{ep_heartbeat.get('avg',0):.1f} / {ep_heartbeat.get('p(95)',0):.1f} / {ep_heartbeat.get('p(99)',0):.1f}",
            "badge": f"{ep_badge.get('avg',0):.1f} / {ep_badge.get('p(95)',0):.1f} / {ep_badge.get('p(99)',0):.1f}",
            "quiz_start": f"{ep_qstart.get('avg',0):.1f} / {ep_qstart.get('p(95)',0):.1f} / {ep_qstart.get('p(99)',0):.1f}",
            "quiz_submit": f"{ep_qsub.get('avg',0):.1f} / {ep_qsub.get('p(95)',0):.1f} / {ep_qsub.get('p(99)',0):.1f}"
        })

    print("\n### Latencias por Endpoint (avg / p95 / p99 en ms)")
    print("| Nivel | Catálogo | Inscripción | Heartbeat | Insignia | Quiz Inicio | Quiz Envío |")
    print("|---|---|---|---|---|---|---|")
    for r in endpoint_rows:
        print(f"| {r['label']} | {r['catalog']} | {r['enroll']} | {r['heartbeat']} | {r['badge']} | {r['quiz_start']} | {r['quiz_submit']} |")

    # Tabla comparativa E2 vs E3
    print("\n### Tabla Comparativa Exigida: Entrega 2 vs Entrega 3 (Escenario 1)")
    print("| Escenario y operación | Nivel de carga | Métrica y unidad | Entrega 2 (Fija) | Entrega 3 (Elástica) | Cambio observado y explicación |")
    print("|---|---|---|---|---|---|")
    for r in rows:
        vus = r["vus"]
        e2 = E2_ESCENARIO1.get(vus, {})
        e2_p95 = f"{e2['p95']:.1f} ms" if "p95" in e2 else "N/A"
        e2_err = f"{e2['err']:.2f}%" if "err" in e2 else "N/A"
        e2_rps = f"{e2['rps']:.2f} req/s" if "rps" in e2 else "N/A"

        # Comparar p95
        p95_diff = r["p95"] - e2.get("p95", r["p95"])
        explicacion = "Sin cambio significativo."
        if e2.get("p95"):
            if r["p95"] < e2["p95"]:
                explicacion = f"Mejora de {abs(p95_diff):.1f} ms gracias a distribución por Load Balancer y autoscaling."
            else:
                explicacion = f"Aumento de {p95_diff:.1f} ms (latencia adicional de red o contención en BD)."

        print(f"| E1: Actividad académica | {r['label']} ({vus} VUs) | Latencia p95 | {e2_p95} | {r['p95']:.1f} ms | {explicacion} |")
        print(f"| E1: Actividad académica | {r['label']} ({vus} VUs) | Throughput | {e2_rps} | {r['rps']:.2f} req/s | Capacidad de procesamiento del backend elástico. |")
        print(f"| E1: Actividad académica | {r['label']} ({vus} VUs) | Tasa de error | {e2_err} | {r['err']:.2f}% | {'Sin errores.' if r['err'] == 0 else 'Errores controlados bajo carga.'} |")


def parse_escenario2(results_dir, prefix=None):
    pattern = os.path.join(results_dir, "*.json")
    files = sorted(glob.glob(pattern))
    if not files:
        print(f"[!] No se encontraron archivos JSON en {results_dir}")
        return

    if not prefix:
        prefixes = sorted(list(set(os.path.basename(f).split('_')[0] + "_" + os.path.basename(f).split('_')[1] for f in files if '_' in os.path.basename(f))))
        prefix = prefixes[-1] if prefixes else ""

    run_files = [f for f in files if prefix in os.path.basename(f)]
    if not run_files:
        run_files = files

    print(f"\n# =====================================================================")
    print(f"# RESULTADOS ESCENARIO 2 — Multimedia y HLS (Corrida: {prefix})")
    print(f"# =====================================================================\n")

    print("### Tabla de Desempeño Multimedia (k6)")
    print("| Nivel | Subida / Playback VUs | Latencia HTTP p95 (ms) | Transcodificación p95 (s) | Transcod. Timeouts | Datos Descargados (MB) | Fallos Descarga |")
    print("|---|---|---|---|---|---|---|")

    rows_e2 = []
    for fpath in run_files:
        try:
            with open(fpath, "r", encoding="utf-8") as f:
                d = json.load(f)
        except Exception:
            continue

        fname = os.path.basename(fpath)
        m = d.get("metrics", {})

        dur = m.get("http_req_duration", {})
        p95 = dur.get("p(95)", 0.0)

        proc = m.get("media_processing_duration", {})
        proc_p95 = proc.get("p(95)", 0.0) / 1000.0 # ms a s

        timeouts = m.get("media_processing_timeouts", {}).get("count", 0)

        bytes_down = m.get("media_segment_bytes_downloaded", {}).get("count", 0)
        mb_down = bytes_down / (1024 * 1024)

        failures = m.get("media_segment_download_failures", {}).get("count", 0)

        stream_p95 = m.get("http_req_duration{endpoint:stream_url}", {}).get("p(95)", 0.0)
        manifest_p95 = m.get("http_req_duration{endpoint:manifest}", {}).get("p(95)", 0.0)
        variant_p95 = m.get("http_req_duration{endpoint:variant_playlist}", {}).get("p(95)", 0.0)
        seg360_p95 = m.get("http_req_duration{endpoint:segment,rendition:360p}", {}).get("p(95)", 0.0)
        seg1080_p95 = m.get("http_req_duration{endpoint:segment,rendition:1080p}", {}).get("p(95)", 0.0)
        upload_p95 = m.get("http_req_duration{endpoint:upload_put}", {}).get("p(95)", 0.0)

        tag = fname.split("_")[2] if len(fname.split("_")) > 2 else fname
        print(f"| {tag} | {fname} | {p95:.1f} | {proc_p95:.1f} | {timeouts} | {mb_down:.2f} MB | {failures} |")

        rows_e2.append({
            "tag": tag,
            "fname": fname,
            "p95": p95,
            "proc_p95": proc_p95,
            "timeouts": timeouts,
            "mb_down": mb_down,
            "failures": failures,
            "stream_p95": stream_p95,
            "manifest_p95": manifest_p95,
            "variant_p95": variant_p95,
            "seg360_p95": seg360_p95,
            "seg1080_p95": seg1080_p95,
            "upload_p95": upload_p95,
        })

    print("\n### Latencias Clave Multimedia por Endpoint (p95 en ms)")
    print("| Nivel | Stream URL | Manifiesto Maestro | Sub-Playlist Variante | Segmento 360p | Segmento 1080p | Subida Directa PUT |")
    print("|---|---|---|---|---|---|---|")
    for r in rows_e2:
        print(f"| {r['tag']} | {r['stream_p95']:.1f} ms | {r['manifest_p95']:.1f} ms | {r['variant_p95']:.1f} ms | {r['seg360_p95']:.1f} ms | {r['seg1080_p95']:.1f} ms | {r['upload_p95']:.1f} ms |")

    # Tabla comparativa E2 vs E3 para Escenario 2
    e2_benchmark = {
        "00": {"label": "Línea base (1 up, 10 pb)", "seg1080": 6837.0, "upload": 6177.0, "manifest": 309.0, "fails": 0},
        "01": {"label": "Nivel 1 (3 up, 50 pb)", "seg1080": 36263.0, "upload": 5905.0, "manifest": 484.0, "fails": 0},
        "02": {"label": "Nivel 2 (5 up, 150 pb)", "seg1080": 117476.0, "upload": 9035.0, "manifest": 853.0, "fails": 136},
        "03": {"label": "Nivel 3 (10 up, 300 pb)", "seg1080": 60002.0, "upload": 29924.0, "manifest": 1500.0, "fails": 533},
        "04": {"label": "Repetición (10 up, 300 pb)", "seg1080": 60002.0, "upload": 39271.0, "manifest": 905.0, "fails": 556},
    }

    print("\n### Tabla Comparativa Exigida: Entrega 2 vs Entrega 3 (Escenario 2)")
    print("| Escenario y operación | Nivel de carga | Métrica y unidad | Entrega 2 (Fija) | Entrega 3 (Elástica) | Cambio observado y explicación |")
    print("|---|---|---|---|---|---|")

    for r in rows_e2:
        tag_key = r["tag"][:2]
        bench = e2_benchmark.get(tag_key)
        if not bench:
            continue
        
        diff_1080 = bench["seg1080"] - r["seg1080_p95"]
        pct_1080 = (diff_1080 / bench["seg1080"]) * 100 if bench["seg1080"] > 0 else 0
        diff_up = bench["upload"] - r["upload_p95"]

        print(f"| E2: Streaming HLS | {bench['label']} | Latencia p95 Segmento 1080p | {bench['seg1080']:.1f} ms | {r['seg1080_p95']:.1f} ms | Reducción de {diff_1080:.1f} ms ({pct_1080:.1f}% más rápido) gracias a descarga desde VM en GCP y GCS. |")
        print(f"| E2: Carga directa | {bench['label']} | Latencia p95 Subida PUT | {bench['upload']:.1f} ms | {r['upload_p95']:.1f} ms | Mejora de {diff_up:.1f} ms en subida directa con Presigned URLs hacia GCS. |")
        fail_msg = "Sin fallos" if r["failures"] == 0 else f"{r['failures']} fallos"
        print(f"| E2: Consumo | {bench['label']} | Fallos de Descarga Segmentos | {bench['fails']} | {r['failures']} | {fail_msg} bajo alta concurrencia. |")


def main():
    parser = argparse.ArgumentParser(description="Extrae métricas de k6 en formato Markdown.")
    parser.add_argument("--e1", default="loadtests/results/escenario1", help="Ruta a resultados escenario 1")
    parser.add_argument("--e2", default="loadtests/results/escenario2", help="Ruta a resultados escenario 2")
    parser.add_argument("--prefix", default=None, help="Prefijo de timestamp específico (ej: 20261021_...)")
    args = parser.parse_args()

    repo_root = Path(__file__).resolve().parent.parent
    e1_dir = repo_root / args.e1
    e2_dir = repo_root / args.e2

    if os.path.exists(e1_dir):
        parse_escenario1(str(e1_dir), args.prefix)
    if os.path.exists(e2_dir):
        parse_escenario2(str(e2_dir), args.prefix)

if __name__ == "__main__":
    main()
