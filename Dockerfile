# pinning, because AWS lightsail AMIs don't support openssh 9+ yet
FROM ruby@sha256:54d09dd38d80d8b850fbff21425d9bd159f9ff7e1de1cdbcbb0b7745f5049784

RUN apt-get -qq update && apt-get -qq -y install openssh-client git >/dev/null
WORKDIR /app
COPY Gemfile .
COPY Gemfile.lock .
RUN bundle install -j 4 --quiet
ADD . .

ENTRYPOINT ["/app/bin/pullpreview"]
