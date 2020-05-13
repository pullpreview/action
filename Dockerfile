FROM ruby:2.7-alpine

RUN apk --no-cache add git openssh-client less
WORKDIR /app
COPY Gemfile .
COPY Gemfile.lock .
RUN bundle install -j 4
ADD . .

ENTRYPOINT ["/app/bin/pullpreview"]
