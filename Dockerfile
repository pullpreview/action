# Pinning, because Amazon Linux 2 doesn't support openssh 9+ clients yet, and we need to maintain compatibility with older pullpreview instances.
# FIXME: Switch back to ruby@3.1-slim after end of august 2023, when we can be sure that newer instances have been created with Amazon Linux 2023.
FROM ruby@sha256:2704d8eede6d399b07e5475cae41f7e7077edd9e970753f543ddb445f7f0424f

RUN apt-get -qq update && apt-get -qq -y install openssh-client git >/dev/null
WORKDIR /app
COPY Gemfile .
COPY Gemfile.lock .
RUN bundle install -j 4 --quiet
ADD . .

ENTRYPOINT ["/app/bin/pullpreview"]
