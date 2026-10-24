# NextDeploy PHP-FPM runtime image. One image per PHP version, shared by every site.
# Deliberately lean: no ImageMagick (WordPress uses GD) and no git; both add 60+ MB.
ARG PHP_VERSION=8.3
FROM php:${PHP_VERSION}-fpm-alpine

ADD https://github.com/mlocati/docker-php-extension-installer/releases/latest/download/install-php-extensions /usr/local/bin/
RUN chmod +x /usr/local/bin/install-php-extensions \
 && install-php-extensions mysqli pdo_mysql opcache gd intl zip bcmath exif soap sockets pcntl redis apcu @composer \
 && apk add --no-cache bash curl unzip zip less mariadb-client \
 && curl -fsSL -o /usr/local/bin/wp https://github.com/wp-cli/wp-cli/releases/download/v2.11.0/wp-cli-2.11.0.phar \
 && chmod +x /usr/local/bin/wp \
 && rm -rf /usr/local/etc/php-fpm.d/* /tmp/* /var/cache/apk/* \
 && mkdir -p /nd/pools /run/php

COPY nd-fpm.conf /usr/local/etc/php-fpm.conf
COPY nd-php.ini /usr/local/etc/php/conf.d/zz-nextdeploy.ini

ENV COMPOSER_HOME=/tmp/.composer WP_CLI_CACHE_DIR=/tmp/.wp-cli/cache
LABEL nextdeploy.php="1"
CMD ["php-fpm", "-F", "-y", "/usr/local/etc/php-fpm.conf"]
