#!/bin/bash
set -e

# Ensure OroRootDir is set
ORO_ROOT_DIR={{.OroRootDir}}
cd ${ORO_ROOT_DIR}

# When the container runs as the host UID/GID (Linux, to keep bind-mounted files
# owned by the host user) that UID often has no /etc/passwd entry. Tools that call
# getpwuid — notably OpenSSH used by `git clone` over ssh:// — abort with
# "No user exists for uid N". Give the current UID a home and a passwd entry.
# Docker may set HOME=/ for a bare numeric --user, which the host UID cannot
# write; fall back to /tmp when HOME is unset or not writable.
if [ -z "$HOME" ] || [ ! -w "$HOME" ]; then
    export HOME=/tmp
fi
CURRENT_UID="$(id -u)"
if ! awk -F: -v uid="$CURRENT_UID" '$3==uid{found=1} END{exit !found}' /etc/passwd; then
    echo "orobox:x:${CURRENT_UID}:$(id -g):orobox:${HOME}:/bin/bash" >> /etc/passwd 2>/dev/null || true
fi

# Enable/Disable Xdebug
if [ "$ORO_XDEBUG_ENABLED" = "true" ] || [ "$ORO_XDEBUG_ENABLED" = "1" ]; then
    if [ -f /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini.disabled ]; then
        mv /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini.disabled /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini
    fi
else
    if [ -f /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini ]; then
        mv /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini.disabled
    fi
fi

# Symfony's runtime takes --env verbatim, so an empty ORO_ENV would abort every console
# command with "the environment cannot be empty". When it is unset, omit the option and
# let the application's .env files decide.
ORO_ENV_OPT=()
if [ -n "$ORO_ENV" ]; then
    ORO_ENV_OPT=( "--env=$ORO_ENV" )
fi

# Case statements for commands
case "$1" in
    nginx)
        exec nginx -g 'daemon off;'
        ;;
    php-fpm)
        exec php-fpm
        ;;
    websocket)
        exec php bin/console gos:websocket:server "${ORO_ENV_OPT[@]}"
        ;;
    consumer)
        exec php bin/console oro:message-queue:consume "${ORO_ENV_OPT[@]}"
        ;;
    cron)
        while true; do
            php bin/console oro:cron "${ORO_ENV_OPT[@]}"
            sleep 60
        done
        ;;
    install)
        shift
        # Build install command options
        INSTALL_OPTS=()
        # Use ORO_APP_URL if defined, otherwise build it from protocol and domain
        if [ -n "$ORO_APP_URL" ]; then
            INSTALL_OPTS+=( "--application-url=${ORO_APP_URL}" )
        elif [ -n "$ORO_APP_PROTOCOL" ] && [ -n "$ORO_APP_DOMAIN" ]; then
            INSTALL_OPTS+=( "--application-url=${ORO_APP_PROTOCOL}://${ORO_APP_DOMAIN}" )
        fi

        [ -n "$ORO_ORGANIZATION_NAME" ] && INSTALL_OPTS+=( "--organization-name=${ORO_ORGANIZATION_NAME}" )
        [ -n "$ORO_USER_NAME" ] && INSTALL_OPTS+=( "--user-name=${ORO_USER_NAME}" )
        [ -n "$ORO_USER_EMAIL" ] && INSTALL_OPTS+=( "--user-email=${ORO_USER_EMAIL}" )
        [ -n "$ORO_USER_FIRSTNAME" ] && INSTALL_OPTS+=( "--user-firstname=${ORO_USER_FIRSTNAME}" )
        [ -n "$ORO_USER_LASTNAME" ] && INSTALL_OPTS+=( "--user-lastname=${ORO_USER_LASTNAME}" )
        [ -n "$ORO_USER_PASSWORD" ] && INSTALL_OPTS+=( "--user-password=${ORO_USER_PASSWORD}" )
        [ -n "$ORO_SAMPLE_DATA" ] && INSTALL_OPTS+=( "--sample-data=${ORO_SAMPLE_DATA}" )
        [ -n "$ORO_LANGUAGE" ] && INSTALL_OPTS+=( "--language=${ORO_LANGUAGE}" )
        [ -n "$ORO_FORMATTING_CODE" ] && INSTALL_OPTS+=( "--formatting-code=${ORO_FORMATTING_CODE}" )

        echo "Running: php bin/console oro:install --no-interaction ${INSTALL_OPTS[*]} $ORO_INSTALL_OPTIONS $*"
        # Fix permissions before install if running as root
        if [ "$(id -u)" = "0" ]; then
            chown -R ${ORO_USER_RUNTIME:-www-data} var/cache var/logs var/data 2>/dev/null || true
            chmod -R 777 var/cache var/logs var/data 2>/dev/null || true
        fi
        rm -rf var/cache/* var/logs/* var/sessions/*
        php bin/console oro:install --no-interaction "${INSTALL_OPTS[@]}" $ORO_INSTALL_OPTIONS "$@"
        STATUS=$?
        if [ $STATUS -ne 0 ]; then
            echo "Error: Installation failed with status $STATUS"
            # Attempt to show any logs if available
            [ -f var/logs/prod.log ] && tail -n 50 var/logs/prod.log
            [ -f var/logs/dev.log ] && tail -n 50 var/logs/dev.log
            exit $STATUS
        fi
        exit 0
        ;;
    nginx-init)
        echo "Nginx init..."
        mkdir -p /opt/oro-nginx/etc/sites-available/
        cp /etc/nginx/http.d/default.conf /opt/oro-nginx/etc/sites-available/oro.conf
        exit 0
        ;;
    *)
        exec "$@"
        ;;
esac
