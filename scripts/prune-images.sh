#!/usr/bin/env sh
# Bound the accumulation of sha-* container versions on ghcr.
#
# Every push publishes five package versions, not one: the multi-arch index
# (the only tagged version), the two platform manifests it points at, and the
# two attestations (provenance and SBOM) attached to it. The four untagged ones
# are NOT orphans — deleting them would break the tagged image that references
# them, edge included. So untagged versions are never deleted on their own
# here: a version is removed only once nothing kept still points at it.
#
# Kept, in this order:
#   - every version carrying a tag that is not sha-* (edge, latest, X.Y, X.Y.Z),
#   - the KEEP most recent sha-*-only versions,
#   - everything the two sets above reference.
# Whatever remains is deleted.
#
# Dry run unless PRUNE_APPLY=1, so the list can be read before anything goes.
# Needs gh (authenticated) and jq. OWNER/PACKAGE/KEEP override the defaults.
set -eu

OWNER="${OWNER:-dmajorel}"
PACKAGE="${PACKAGE:-moxy}"
KEEP="${KEEP:-20}"
REGISTRY="${REGISTRY:-ghcr.io}"

for tool in gh jq; do
	command -v "$tool" >/dev/null 2>&1 || {
		echo "$tool is required" >&2
		exit 2
	}
done

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT INT TERM

echo "==> listing versions of $REGISTRY/$OWNER/$PACKAGE"
gh api --paginate "/users/$OWNER/packages/container/$PACKAGE/versions" \
	--jq '.[] | {id, digest: .name, created: .created_at, tags: (.metadata.container.tags // [])}' \
	>"$WORK/versions.jsonl"

total="$(wc -l <"$WORK/versions.jsonl" | tr -d ' ')"
echo "==> $total versions"

# Tagged with something other than sha-*: a release or a moving pointer. Never
# deleted, however old — a semver tag is what someone else's deployment pins.
jq -r 'select(.tags | length > 0) | select(any(.tags[]; startswith("sha-") | not)) | .digest' \
	<"$WORK/versions.jsonl" >"$WORK/keep-roots"

# The KEEP most recent versions whose tags are all sha-*. The API returns
# versions newest first, so head is the recent end.
jq -r 'select(.tags | length > 0) | select(all(.tags[]; startswith("sha-"))) | .digest' \
	<"$WORK/versions.jsonl" | head -n "$KEEP" >>"$WORK/keep-roots"

sort -u "$WORK/keep-roots" -o "$WORK/keep-roots"
echo "==> $(wc -l <"$WORK/keep-roots" | tr -d ' ') kept roots"

# A pull token, to expand an index into the manifests it references. The
# package is private, so this exchange has to be authenticated; GHCR_TOKEN is
# what the workflow passes, gh auth token is the local fallback. Read access is
# all that is needed here — deletion goes through the API above.
GHCR_TOKEN="${GHCR_TOKEN:-$(gh auth token)}"
TOKEN="$(curl -fsSL -u "$OWNER:$GHCR_TOKEN" \
	"https://$REGISTRY/token?scope=repository:$OWNER/$PACKAGE:pull&service=$REGISTRY" | jq -r .token)"
[ -n "$TOKEN" ] && [ "$TOKEN" != "null" ] || {
	echo "could not obtain a registry pull token" >&2
	exit 1
}

# Expand each root into everything it references. A failure here aborts before
# any deletion: a half-resolved index would make a referenced child look like an
# orphan, and that is exactly the mistake this script exists to avoid.
: >"$WORK/keep"
while read -r digest; do
	echo "$digest" >>"$WORK/keep"
	curl -fsSL \
		-H "Authorization: Bearer $TOKEN" \
		-H "Accept: application/vnd.oci.image.index.v1+json,application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.v2+json" \
		"https://$REGISTRY/v2/$OWNER/$PACKAGE/manifests/$digest" >"$WORK/manifest.json" || {
		echo "could not read the manifest of $digest; aborting without deleting" >&2
		exit 1
	}
	jq -r '(.manifests // [])[].digest, (.subject.digest // empty)' <"$WORK/manifest.json" >>"$WORK/keep"
done <"$WORK/keep-roots"

sort -u "$WORK/keep" -o "$WORK/keep"
echo "==> $(wc -l <"$WORK/keep" | tr -d ' ') versions kept, roots and children"

jq -r '[.id, .digest, (.tags | join(","))] | @tsv' <"$WORK/versions.jsonl" >"$WORK/all.tsv"

deleted=0
while IFS='	' read -r id digest tags; do
	grep -qxF "$digest" "$WORK/keep" && continue
	deleted=$((deleted + 1))
	if [ "${PRUNE_APPLY:-0}" = "1" ]; then
		echo "delete $digest [${tags:-untagged}]"
		gh api -X DELETE "/users/$OWNER/packages/container/$PACKAGE/versions/$id" >/dev/null
	else
		echo "would delete $digest [${tags:-untagged}]"
	fi
done <"$WORK/all.tsv"

if [ "${PRUNE_APPLY:-0}" = "1" ]; then
	echo "==> deleted $deleted of $total"
else
	echo "==> would delete $deleted of $total (set PRUNE_APPLY=1 to apply)"
fi
