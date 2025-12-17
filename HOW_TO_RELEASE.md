# Release process

## Preparing the branch

Checkout the release branch and merge main or cherry pick the relevant commits:

```bash
git checkout v0.9
git merge main
git push
```

### Using cherry picks

In case only a subset of the changes are brought to the new release, cherry-pick
must be used.

```bash
git checkout v0.9
git cherry-pick -x f1f86ed658c1e8a6f90f967ed94881d61476b4c0
git push
```

## Generate the release notes

A convenience generator script is available at `hack/gen_relnotes.sh`. The script can be run in two modes:

### Automatic mode (recommended)

```bash
hack/gen_relnotes.sh
```

This will automatically:
- Find the previous release commit (searches for "Prepare the v*" commits)
- Use the HEAD of `upstream/main` as the end point
- Auto-increment the patch version from the current release
- Generate categorized release notes based on PR labels (`kind/feature`, `kind/bug`, `kind/cleanup`)
- Update `RELEASE_NOTES.md` with the new release section

### Manual mode with custom version

```bash
hack/gen_relnotes.sh 0.0.21
```

This will use the provided version number instead of auto-incrementing.

### Prerequisites

The `GITHUB_TOKEN` environment variable must be set with a GitHub token which has the following permissions:

Read access to:

- Contents
- Pull requests
- Commit statuses

## Finalize the release notes

All release notes are always written on the main branch in the `RELEASE_NOTES.md` file, and copied into release branches in a later step.

After running the generation script, review the generated notes and:
- Point out all new features and actions required by users
- Highlight notable bugfixes (e.g. security issues, long-term pain points resolved)
- Verify all PRs are properly categorized
- Add any additional context or explanations as needed

To get a list of contributors to the release, run `git log --format="%aN" $(git merge-base CUR-BRANCH PREV-TAG)..CUR-BRANCH | sort -u | tr '\n' ',' | sed -e 's/,/, /g'`. CUR-BRANCH is main if you’re making a minor release (e.g. 0.9.0), or the release branch for the current version if you’re making a patch release (e.g. v0.8 if you’re making v0.8.4). PREV-TAG is the release tag name for the last release (e.g. if you’re preparing 0.8.4, PREV-TAG is v0.8.3. Also think about whether there were significant contributions that weren’t in the form of a commit, and include those people as well. It’s better to err on the side of being too thankful!

Commit the finalized release notes.

## Clean the working directory

The release script only works if the Git working directory is completely clean: no pending modifications, no untracked files, nothing. Make sure everything is clean, or run the release from a fresh checkout.

The release script will abort if the working directory isn’t right.

## Run the release script
Run `FRRK8S_VERSION="X.Y.Z" make cutrelease` from the main branch. This will create the appropriate branches, commits and tags in your local repository.

## Push the new artifacts
Run git push --tags origin main `vX.Y`. This will push all pending changes both in main and the release branch, as well as the new tag for the release.

## Wait for the image repositories to update
When you pushed, GitHub actions kicked off a set of image builds for the new tag. You need to wait for these images to be pushed live before creating a new release. Check on quay.io that the tagget version exists.

## Create a new release on github
By default, new tags show up de-emphasized in the list of releases. Create a new release attached to the tag you just pushed. Make the description point to the release notes on the website.
