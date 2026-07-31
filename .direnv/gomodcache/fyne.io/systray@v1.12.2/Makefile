tag-changelog: require-version require-gh-token
	echo "Tagging..." && \
	git tag -a "$$VERSION" -f --annotate -m"Tagged $$VERSION" && \
	git push --tags -f && \
	git checkout master && \
	git pull && \
	github_changelog_generator --no-issues --max-issues 100 --token "${GH_TOKEN}" --user getlantern --project systray && \
	git add CHANGELOG.md && \
	git commit -m "Updated changelog for $$VERSION" && \
	git push origin HEAD && \
	git checkout -

guard-%:
	 @ if [ -z '${${*}}' ]; then echo 'Environment variable $* not set' && exit 1; fi

require-version: guard-VERSION

require-gh-token: guard-GH_TOKEN
