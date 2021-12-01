FROM ruby:2.7-slim

RUN apt-get update -qq && apt-get install openssh-client git -y
WORKDIR /app
COPY Gemfile .
COPY Gemfile.lock .
RUN bundle install -j 4
ADD . .

ENTRYPOINT ["/app/bin/pullpreview"]
