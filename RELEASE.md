# Making a release

Compile and test

Then run

    goreleaser --clean --snapshot

To test the build

When happy, tag the release

    git tag -s v1.0.XX -m "Release v1.0.XX"

Push it to github with

    git push origin # without --follow-tags so it doesn't push the tag if it fails
    git push --follow-tags origin

Then do a release build (set GITHUB token first)

  goreleaser --clean

