#!/usr/bin/env sh
# Prepare a tagged release.
#
# There is only one version in this repository and it is the tag: build.sh asks
# git describe and injects the answer at link time, so `moxyd -version`,
# /healthz, the container label and the ghcr tags all say the same thing because
# they all read that tag. Tagging therefore *is* the release, and everything
# that must hold for a release has to hold before the tag exists. This script
# checks those things, then creates the annotated tag.
#
# Dry run unless RELEASE_APPLY=1 — the same contract as prune-images.sh. Without
# it nothing in the repository is written: the script verifies and prints the
# commands it would run. Pushing the tag is left to a human in both cases, since
# that push is what publishes the image.
#
# The procedure this script enforces is documented in docs/RELEASE.md.
set -eu

ROOT="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
VERSION="${1:-}"
APPLY="${RELEASE_APPLY:-0}"
BRANCH="${RELEASE_BRANCH:-main}"
NOTES="$ROOT/bin/release-notes-${VERSION}.md"

usage() {
	cat >&2 <<'EOF'
usage: scripts/release.sh vX.Y.Z

Checks that the tree is ready to be tagged, builds the binary with that version
and verifies it reports it, then prints the tag commands.

  RELEASE_APPLY=1   also create the annotated tag (default: dry run)
  RELEASE_BRANCH    branch a release may be cut from (default: main)
EOF
	exit 2
}

failed=0
step() { printf '==> %s\n' "$*"; }
bad() {
	printf 'FAIL: %s\n' "$*" >&2
	failed=1
}
warn() { printf 'warning: %s\n' "$*" >&2; }
# What blocks a release but not a rehearsal: in a dry run these are reported and
# the rest of the checks still run, which is the point of rehearsing.
strict() {
	if [ "$APPLY" = "1" ]; then bad "$@"; else warn "$@"; fi
}

[ -n "$VERSION" ] || usage
case "$VERSION" in
-*) usage ;;
esac

# --- The version string ----------------------------------------------------
# vX.Y.Z, optionally with a pre-release suffix (v0.1.0-rc.1). The leading v is
# what the image workflow matches on, and what tells a release tag apart from
# any other tag someone may push.
if ! printf '%s\n' "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'; then
	printf 'FAIL: %s is not a vX.Y.Z version\n' "$VERSION" >&2
	exit 1
fi

step "release $VERSION ($([ "$APPLY" = "1" ] && echo 'apply' || echo 'dry run'))"

# --- The repository --------------------------------------------------------
git -C "$ROOT" rev-parse --git-dir >/dev/null 2>&1 || {
	printf 'FAIL: not a git repository: %s\n' "$ROOT" >&2
	exit 1
}

step 'repository state'
if git -C "$ROOT" rev-parse -q --verify "refs/tags/$VERSION" >/dev/null; then
	bad "tag $VERSION already exists"
fi

if [ -n "$(git -C "$ROOT" status --porcelain)" ]; then
	strict 'the working tree is dirty; a release is cut from a committed tree'
fi

current="$(git -C "$ROOT" rev-parse --abbrev-ref HEAD)"
if [ "$current" != "$BRANCH" ]; then
	strict "HEAD is on $current, not $BRANCH"
fi

# The tag is only worth something if the commit it names is the one reviewed and
# built by CI. An unpushed HEAD would publish an image nobody can check out.
if git -C "$ROOT" rev-parse -q --verify "refs/remotes/origin/$BRANCH" >/dev/null; then
	if [ "$(git -C "$ROOT" rev-parse HEAD)" != "$(git -C "$ROOT" rev-parse "refs/remotes/origin/$BRANCH")" ]; then
		strict "HEAD is not origin/$BRANCH (fetch, then release from an up-to-date branch)"
	fi
fi

# A tag that does not look like a version still wins a plain `git describe`, and
# would then become the version of every build until the next tag. Until the
# build scripts pass --match 'v*', the only defence is not to have such tags.
stray="$(git -C "$ROOT" tag | grep -v '^v[0-9]' || true)"
if [ -n "$stray" ]; then
	warn "tags that are not versions exist and may shadow $VERSION in git describe:"
	printf '  %s\n' $stray >&2
fi

# --- The files a release must carry ----------------------------------------
step 'LICENSE'
[ -s "$ROOT/LICENSE" ] || bad 'LICENSE is missing or empty'

step 'CHANGELOG.md'
heading="$(awk -v v="$VERSION" '$1 == "##" && $2 == v { print; exit }' "$ROOT/CHANGELOG.md" 2>/dev/null || true)"
if [ -z "$heading" ]; then
	bad "CHANGELOG.md has no '## $VERSION' section"
else
	case "$heading" in
	*' — '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]) ;;
	*) strict "CHANGELOG.md: date the section — '## $VERSION — $(date -u +%Y-%m-%d)'" ;;
	esac
	mkdir -p "$(dirname -- "$NOTES")"
	# The section body, stripped of its leading and trailing blank lines: this is
	# what --notes-file hands to `gh release create`, with the generated list of
	# merged pull requests appended under it.
	awk -v v="$VERSION" '
		/^## / { inside = ($2 == v); next }
		inside { body = body $0 "\n" }
		END {
			sub(/^\n+/, "", body)
			sub(/\n+$/, "\n", body)
			printf "%s", body
		}
	' "$ROOT/CHANGELOG.md" >"$NOTES"
	[ -s "$NOTES" ] || bad "the '## $VERSION' section of CHANGELOG.md is empty"
fi

# --- The build -------------------------------------------------------------
# Everything the repository promises, verified on this commit rather than after
# the tag is public.
step 'checks'
"$ROOT/scripts/check.sh" || bad 'checks failed'

# The version is forced here to what the tag will make git describe say: this is
# the single assertion that the string in the binary, in /healthz and in the
# image label is the one being released.
step 'build'
VERSION="$VERSION" "$ROOT/scripts/build.sh" || bad 'build failed'

step 'version reported by the binary'
reported="$("$ROOT/bin/moxyd" -version 2>/dev/null || true)"
if [ "$reported" != "moxyd $VERSION" ]; then
	bad "the binary reports '$reported', expected 'moxyd $VERSION'"
else
	printf '    %s\n' "$reported"
fi

if [ "$failed" != "0" ]; then
	printf '\nnot ready to release %s\n' "$VERSION" >&2
	exit 1
fi

# --- The tag ---------------------------------------------------------------
# Annotated, never lightweight: an annotated tag carries an author, a date and a
# message, and `git describe` prefers it. The message is the one line a reader
# of `git tag -n` gets.
message="moxy $VERSION"

if [ "$APPLY" != "1" ]; then
	cat <<EOF

==> ready to release $VERSION
    release notes: $NOTES

Run, in this order:

    RELEASE_APPLY=1 ./scripts/release.sh $VERSION
    git push origin $VERSION
    gh release create $VERSION --title "$message" --notes-file "$NOTES" --generate-notes

EOF
	exit 0
fi

step "git tag -a $VERSION"
git -C "$ROOT" tag -a "$VERSION" -m "$message"

# git describe is what the build reads. Now that the tag exists, ask it the same
# question the build will ask, and refuse to leave a tag behind that answers
# something else — a stray tag on the same commit would do exactly that.
described="$(git -C "$ROOT" describe --tags --always)"
if [ "$described" != "$VERSION" ]; then
	git -C "$ROOT" tag -d "$VERSION" >/dev/null
	printf 'FAIL: git describe says %s, not %s; tag removed\n' "$described" "$VERSION" >&2
	exit 1
fi

cat <<EOF

==> $VERSION tagged locally, nothing published yet
    release notes: $NOTES

Publishing is the push, and it is yours to make:

    git push origin $VERSION
    gh release create $VERSION --title "$message" --notes-file "$NOTES" --generate-notes

EOF
