FROM ruby:3.2-alpine

WORKDIR /app

COPY Gemfile ./
RUN bundle install

COPY app.rb ./

CMD ["ruby", "app.rb"]
