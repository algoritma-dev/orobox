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
        APP_URL=""
        if [ -n "$ORO_APP_URL" ]; then
            APP_URL="$ORO_APP_URL"
        elif [ -n "$ORO_APP_PROTOCOL" ] && [ -n "$ORO_APP_DOMAIN" ]; then
            APP_URL="${ORO_APP_PROTOCOL}://${ORO_APP_DOMAIN}"
        fi
        # An `[ ... ] && ...` one-liner would abort the whole entrypoint under `set -e` whenever
        # the test is false, which is exactly the case this has to tolerate.
        if [ -n "$APP_URL" ]; then
            INSTALL_OPTS+=( "--application-url=${APP_URL}" )
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

        # Fix permissions before install if running as root
        if [ "$(id -u)" = "0" ]; then
            chown -R ${ORO_USER_RUNTIME:-www-data} var/cache var/logs var/data 2>/dev/null || true
            chmod -R 777 var/cache var/logs var/data 2>/dev/null || true
        fi
        rm -rf var/cache/* var/logs/* var/sessions/*

        # A published image carries a dump of an OroCommerce already installed for its own
        # version, next to the identity that install was given. Restoring that dump and
        # reconciling the schema with oro:platform:update reaches the same database oro:install
        # would build, in about a third of the time — and unlike oro:install it does not have to
        # replay every migration the platform has ever shipped.
        #
        # The seed is taken without sample data, because that is what the QA and functional test
        # databases need; a request for sample data is served by loading the demo fixtures on top,
        # which is what oro:install does for --sample-data=y in the first place.
        #
        # Every reason it can fail — no dump for this Postgres major, an install identity that
        # does not match the one baked in, a dump that does not restore, an update that does not
        # reconcile — falls back to the install that was always here, after emptying the schema
        # the failed attempt may have half-written. Set ORO_NO_SEED=1 to skip it outright.
        SEED_DUMP='{{.SeedDumpPath}}'
        SEED_IDENTITY="${SEED_DUMP%.sql.gz}.identity"

        seed_psql() {
            PGPASSWORD="$ORO_DB_PASSWORD" psql -h "$ORO_DB_HOST" -p "${ORO_DB_PORT:-5432}" \
                -U "$ORO_DB_USER" -d "$ORO_DB_NAME" -v ON_ERROR_STOP=1 -q "$@"
        }

        seed_reset_schema() {
            seed_psql -c 'DROP SCHEMA IF EXISTS public CASCADE' \
                -c 'CREATE SCHEMA public' \
                -c 'CREATE EXTENSION IF NOT EXISTS "uuid-ossp"' \
                -c 'CREATE EXTENSION IF NOT EXISTS pg_trgm'
        }

        # The restored rows carry the administrator, the organization and the locale the bake
        # chose. Reusing the dump for an install that asked for different ones would hand back a
        # database that quietly disagrees with the options it was given — most visibly a login
        # that is not the one in .env — so that install builds its own instead.
        seed_identity() {
            printf '%s|%s|%s|%s|%s|%s|%s|%s' \
                "${ORO_USER_NAME-}" "${ORO_USER_PASSWORD-}" "${ORO_USER_EMAIL-}" \
                "${ORO_USER_FIRSTNAME-}" "${ORO_USER_LASTNAME-}" "${ORO_ORGANIZATION_NAME-}" \
                "${ORO_LANGUAGE-}" "${ORO_FORMATTING_CODE-}"
        }

        seed_install() {
            if [ -n "$ORO_NO_SEED" ]; then return 1; fi
            if [ ! -f "$SEED_DUMP" ] || [ ! -f "$SEED_IDENTITY" ]; then return 1; fi
            if [ -z "$ORO_DB_HOST" ] || [ -z "$ORO_DB_NAME" ] || [ -z "$ORO_DB_USER" ]; then return 1; fi
            if ! command -v psql >/dev/null 2>&1; then return 1; fi
            if [ "$(seed_identity)" != "$(cat "$SEED_IDENTITY")" ]; then
                echo "The image's pre-installed database was built for a different administrator or locale; installing from scratch."
                return 1
            fi

            echo "Restoring the pre-installed database the image carries instead of running oro:install."
            # From here on the schema is being rewritten, so a failure has to be cleaned up before
            # oro:install can take over. Nothing above this line has touched the database.
            SEED_TOUCHED=1
            seed_reset_schema || return 1
            # pipefail in a subshell rather than for the whole entrypoint: a truncated dump makes
            # gunzip fail, and psql's own status would otherwise be the only one read.
            ( set -o pipefail; gunzip -c "$SEED_DUMP" | seed_psql >/dev/null ) || return 1

            # The dump holds the schema and the oro_entity_config rows, but none of the generated
            # extend classes that make those fields reachable: without this the first query on an
            # extend field dies on a column that is plainly in the database.
            php bin/console oro:platform:update --force --timeout=0 --skip-download-translations --skip-translations || return 1

            # The seed was installed against the bake's placeholder URL.
            if [ -n "$APP_URL" ]; then
                seed_psql -c "UPDATE oro_config_value SET text_value = '${APP_URL}' WHERE name IN ('application_url', 'url', 'secure_url')" || return 1
            fi

            case "${ORO_SAMPLE_DATA:-n}" in
                y|Y|yes|Yes|YES)
                    echo "Loading the sample data on top of the restored database."
                    php bin/console oro:migration:data:load --fixtures-type=demo --no-interaction || return 1
                    ;;
            esac
            return 0
        }

        SEED_TOUCHED=""
        if seed_install; then
            echo "Database ready from the image's pre-installed dump."
            exit 0
        fi
        if [ -n "$SEED_TOUCHED" ]; then
            # The attempt got as far as rewriting the schema, so it left tables oro:install would
            # trip over. Only this case resets: an install that never reached the database must
            # keep meeting whatever was already there, on its own terms.
            echo "The seed restore did not complete; emptying the database and installing from scratch."
            seed_reset_schema || true
        fi

        echo "Running: php bin/console oro:install --no-interaction ${INSTALL_OPTS[*]} $ORO_INSTALL_OPTIONS $*"
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
