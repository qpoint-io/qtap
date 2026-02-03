FROM ruby:3.2-alpine

# Install MySQL client libraries for mysql2 gem
RUN apk add --no-cache build-base mysql-dev

WORKDIR /app

COPY Gemfile ./
RUN bundle install

COPY app.rb ./

CMD ["ruby", "app.rb"]
