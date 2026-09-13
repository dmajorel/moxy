#!/usr/bin/env sh
# Bound the accumulation of sha-* container versions on ghcr.
#
# Every push publishes six package versions, not one: the multi-arch index, the
# two platform manifests it points at, the two attestations (provenance and
# SBOM) attached to it, and the referrer that ghcr surfaces under a
# sha256-<digest> tag. The four untagged ones are NOT orphans — deleting them
# would break the tagged image that references them, edge included. So untagged
# versions are never deleted on their own here: a version is removed only once
# nothing kept still points at it.
#
# Kept, in this order:
#   - every version carrying a release tag (X.Y.Z, X.Y, latest),
#   - the KEEP most recent rolling versions (sha-<commit>, and the edge on top
#     of the newest of them), counted once per variant: the -debug images roll
#     on tags of their own and must not eat the window that bounds the shipped
#     image,
#   - everything the two sets above reference,
#   - the sha256-<digest> attestation pointers of everything kept.
# Whatever remains is deleted.
#
# Dry run unless PRUNE_APPLY=1, so the list can be read before anything goes.
# Needs gh (authenticated) and jq. OWNER/PACKAGE/KEEP override the defaults.
set -eu

OWNER="${OWNER:-dmajorel}"
PACKAGE="${PACKAGE:-moxy}"
KEEP="${KEEP:-5}"
REGISTRY="${REGISTRY:-ghcr.io}"

for tool in gh jq; do
	command -v "$tool" >/dev/null 2>&1 || {
		echo "$tool is required" >&2
		exit 2
	}
done

# What counts as a release tag, in the one place both selections below read it.
RELEASE='test("^v?[0-9]+(\\.[0-9]+)*$") or . == "latest"'

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT INT TERM

echo "==> listing versions of $REGISTRY/$OWNER/$PACKAGE"
gh api --paginate "/users/$OWNER/packages/container/$PACKAGE/versions" \
	--jq '.[] | {id, digest: .name, created: .created_at, tags: (.metadata.container.tags // [])}' \
	>"$WORK/versions.jsonl"

total="$(wc -l <"$WORK/versions.jsonl" | tr -d ' ')"
echo "==> $total versions"

# A release tag is never deleted, however old: X.Y.Z, X.Y and latest are what
# someone else's deployment pins.
jq -r "select(any(.tags[]; $RELEASE)) | .digest" \
	<"$WORK/versions.jsonl" >"$WORK/keep-roots"

# The KEEP most recent rolling versions. edge belongs here rather than among the
# permanent roots above: it names the newest main build and moves at every push,
# so keeping it apart would quietly hold a sixth image past the KEEP promised.
# sha256-<digest> is left out too, and for the opposite reason — it is not an
# image but an attestation naming the one it attests, and it follows its subject
# below. Counted as a root it would make every attested image permanent, and the
# sha-* versions this script exists to bound would never be pruned at all.
# The API returns versions newest first, so head is the recent end.
ROLLING="select(.tags | length > 0)
	| select(any(.tags[]; (startswith(\"sha256-\") or $RELEASE) | not))
	| .digest"

# Selected once per variant. The debug image rolls on tags of its own
# (edge-debug, X.Y.Z-debug), and none of them matches the release pattern that
# makes a version permanent: sharing the single window would halve the KEEP the
# shipped image promises, since a push now publishes two images, while dropping
# the variant from the selection would delete it minutes after the publish job
# that pushed it — this script keeps an untagged version only when something
# kept still points at it.
DEBUG='any(.tags[]; endswith("-debug"))'
jq -r "select($DEBUG | not) | $ROLLING" <"$WORK/versions.jsonl" |
	head -n "$KEEP" >>"$WORK/keep-roots"
jq -r "select($DEBUG) | $ROLLING" <"$WORK/versions.jsonl" |
	head -n "$KEEP" >>"$WORK/keep-roots"

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

# An attestation is tagged sha256-<digest of its subject>, so which ones survive
# is a tag comparison, no second manifest read: keep those whose subject is kept
# and let the rest go with the image they describe.
sed 's/^sha256:/sha256-/' "$WORK/keep" >"$WORK/keep-pointers"
jq -r 'select(.tags | length > 0) | .digest as $d | .tags[] | [., $d] | @tsv' \
	<"$WORK/versions.jsonl" >"$WORK/tag-index"
awk -F'\t' 'NR == FNR { subject[$0]; next } ($1 in subject) { print $2 }' \
	"$WORK/keep-pointers" "$WORK/tag-index" >>"$WORK/keep"

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
