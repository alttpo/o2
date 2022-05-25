#!/bin/bash
# accept argument; ideally should look like v0.0.100-rc1 to trigger a pre-release for goreleaser -> github
newtag="$1"

echo "New patch-bumped tag: ${newtag}"
git tag -a "${newtag}" -m "${newtag}"

read -p "Press enter to push --tags or ^C to abort"
git push --tags
