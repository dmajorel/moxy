#!/usr/bin/env sh
# Delete the review images a pull request left behind.
#
# The Image workflow hands every code-touching pull request a loadable image,
# as a workflow artifact named moxy-pr-<number>-<short sha>. Once the branch is
# closed the image has no reader left: what it showed is either on main or
# abandoned. The artifacts have a short retention of their own, but a merged
# branch should not wait for it.
#
# An artifact belongs to a RUN, never to a pull request, so there is nothing to
# ask the API for directly: the name is the only link back, which is why it
# carries the number. A branch pushed five times left five of them.
#
# Failing to delete one is not a failure worth reporting: it may have expired,
# or its run may already have been swept. The retention is the backstop, and
# closing a pull request must not go red over housekeeping. Only a malformed
# request stops the script.
#
# Needs gh (authenticated, with actions: write) and jq. PR is required; REPO
# defaults to this repository.
set -eu

: "${PR:?PR (the pull request number) is required}"
REPO="${REPO:-dmajorel/moxy}"

# The number reaches us from a workflow input and is interpolated into a jq
# filter below. It is github.event.number, so it is an integer — but a filter
# built from an unchecked string is a habit worth not having.
case "$PR" in
'' | *[!0-9]*)
	echo "PR must be a number, got: $PR" >&2
	exit 2
	;;
esac

for tool in gh jq; do
	command -v "$tool" >/dev/null 2>&1 || {
		echo "$tool is required" >&2
		exit 2
	}
done

# The trailing dash matters: without it, closing #23 would sweep #230 too.
prefix="moxy-pr-${PR}-"

# --paginate without --jq yields one JSON object per page, which jq reads as a
# stream; --arg keeps the prefix out of the filter's source.
ids=$(gh api --paginate "repos/${REPO}/actions/artifacts" |
	jq -r --arg prefix "$prefix" \
		'.artifacts[] | select(.name | startswith($prefix)) | .id')

if [ -z "$ids" ]; then
	echo "no review image left for pull request #${PR}"
	exit 0
fi

deleted=0
failed=0
for id in $ids; do
	if gh api -X DELETE "repos/${REPO}/actions/artifacts/${id}" >/dev/null 2>&1; then
		deleted=$((deleted + 1))
	else
		failed=$((failed + 1))
		echo "could not delete artifact ${id}; it has probably expired" >&2
	fi
done

echo "deleted ${deleted} review image(s) of pull request #${PR}, ${failed} left to the retention"
