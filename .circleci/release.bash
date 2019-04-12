#!/bin/bash

set -euo pipefail
# Opinionated script to release on GitHub.
# This script runs in CircleCI, in a golang docker container from a folder that is a git repo.
# The script was triggered because of a git tag push. CIRCLE_TAG contains the tag pushed.
# The script expects the binaries to reside in the build folder.
STOML_VERSION=0.3.0

# Create GitHub release draft
draftdata="
{
  \"tag_name\": \"${CIRCLE_TAG}\",
  \"target_commitish\": \"${CIRCLE_BRANCH}\",
  \"name\": \"${CIRCLE_TAG}\",
  \"body\": \"${CIRCLE_TAG} release.\",
  \"draft\": true,
  \"prerelease\": false
}
"
curl -s -S -X POST -u "${GITHUB_USERNAME}:${GITHUB_TOKEN}" https://api.github.com/repos/${CIRCLE_PROJECT_USERNAME}/${CIRCLE_PROJECT_REPONAME}/releases --user-agent releasebot -H "Accept: application/vnd.github.v3.json" -d "$draftdata" > draft.json
ERR=$?
if [[ $ERR -ne 0 ]]; then
  echo "ERROR: curl error, exitcode $ERR."
  exit $ERR
fi

wget -q "https://github.com/freshautomations/stoml/releases/download/v${STOML_VERSION}/stoml_linux_amd64"
chmod +x ./stoml_linux_amd64
export id="`./stoml_linux_amd64 draft.json id`"
if [ -z "$id" ]; then
  echo "ERROR: Could not get draft id."
  exit 1
fi

echo "Release ID: ${id}"

# Upload binaries

for binary in tm-load-test
do
echo -ne "Processing ${binary}... "
if [[ ! -f "build/${binary}" ]]; then
  echo "${binary} does not exist."
  continue
fi
curl -s -S -X POST -u "${GITHUB_USERNAME}:${GITHUB_TOKEN}" "https://uploads.github.com/repos/${CIRCLE_PROJECT_USERNAME}/${CIRCLE_PROJECT_REPONAME}/releases/${id}/assets?name=${binary}" --user-agent releasebot -H "Accept: application/vnd.github.v3.raw+json" -H "Content-Type: application/octet-stream" -H "Content-Encoding: utf8" --data-binary "@build/${binary}" > upload.json
ERR=$?
if [[ $ERR -ne 0 ]]; then
  echo "ERROR: curl error, exitcode $ERR."
  exit $ERR
fi

export uid="`./stoml_linux_amd64 upload.json id`"
if [ -z "$uid" ]; then
  echo "ERROR: Could not get upload id for binary ${binary}."
  exit 1
fi

echo "uploaded binary ${binary}, id ${uid}."
done

rm draft.json
rm upload.json

# Publish release
releasedata="
{
  \"draft\": false,
  \"tag_name\": \"${CIRCLE_TAG}\"
}
"
curl -s -S -X POST -u "${GITHUB_USERNAME}:${GITHUB_TOKEN}" "https://api.github.com/repos/${CIRCLE_PROJECT_USERNAME}/${CIRCLE_PROJECT_REPONAME}/releases/${id}" --user-agent script -H "Accept: application/vnd.github.v3.json" -d "$releasedata"
ERR=$?
if [[ $ERR -ne 0 ]]; then
  echo "ERROR: curl error, exitcode $ERR."
  exit $ERR
fi

