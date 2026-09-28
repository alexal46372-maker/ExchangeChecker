#!/usr/bin/env bash
# Commits ../ci-report/*.txt as reports/<sha>/<job>-<ref>-<run>-<attempt>.txt to branch ci-reports
# via the GitHub contents API (text only; no binaries / build artifacts).
set -u
REPORT=../ci-report/report.txt
{
  echo "run: ${GITHUB_SERVER_URL}/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID} attempt ${GITHUB_RUN_ATTEMPT}"
  echo "sha: ${GITHUB_SHA}  ref: ${GITHUB_REF}  event: ${GITHUB_EVENT_NAME}  job: ${JOB_NAME}"
  echo "outcomes: ${OUTCOMES}"
  for f in version vet test build; do
    if [ -f "../ci-report/$f.txt" ]; then
      echo "===== $f"
      cat "../ci-report/$f.txt"
    fi
  done
} > "$REPORT"
ref_name="${GITHUB_REF_NAME//\//_}"
path="reports/${GITHUB_SHA}/${JOB_NAME}-${ref_name}-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}.txt"
b64=$(base64 < "$REPORT" | tr -d '\n\r')
printf '{"message":"ci report %s %s","branch":"ci-reports","content":"%s"}' "${GITHUB_SHA:0:8}" "$JOB_NAME" "$b64" > ../ci-report/body.json
for i in 1 2 3 4 5; do
  code=$(curl -sS -o ../ci-report/resp.json -w '%{http_code}' -X PUT \
    -H "Authorization: Bearer ${GH_TOKEN}" -H "Accept: application/vnd.github+json" \
    --data @../ci-report/body.json \
    "${GITHUB_API_URL}/repos/${GITHUB_REPOSITORY}/contents/${path}")
  echo "publish attempt $i: HTTP $code"
  if [ "$code" = "201" ] || [ "$code" = "200" ]; then exit 0; fi
  sleep $((i * 3))
done
cat ../ci-report/resp.json || true
exit 0
