# Pinning, because Amazon Linux 2 doesn't support openssh 9+ clients yet, and we need to maintain compatibility with older pullpreview instances.
# FIXME: Switch back to ruby@3.1-slim after end of august 2023, when we can be sure that newer instances have been created with Amazon Linux 2023.
FROM ruby@sha256:54d09dd38d80d8b850fbff21425d9bd159f9ff7e1de1cdbcbb0b7745f5049784

RUN apt-get -qq update && apt-get -qq -y install openssh-client git >/dev/null
WORKDIR /app
COPY Gemfile .
COPY Gemfile.lock .
RUN bundle install -j 4 --quiet
ADD . .

ENTRYPOINT ["/app/bin/pullpreview"]
