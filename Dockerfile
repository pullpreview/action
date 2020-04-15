FROM ruby:2-alpine

RUN apk --no-cache add git openssh-client
RUN gem install bundler
WORKDIR /app
COPY Gemfile .
COPY Gemfile.lock .
RUN bundle install -j 4
ADD . .

ENTRYPOINT ["/app/bin/pullpreview"]
