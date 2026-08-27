<?php

declare(strict_types=1);

namespace Orobox\Bundle\E2ETestBundle;

/**
 * A class with no dependencies of its own, so a test can prove the bundle's namespace resolves
 * through the application's autoloader without loading anything from Symfony. The bundle class
 * next to it extends Symfony's Bundle and would drag the framework into a unit test.
 */
class Marker
{
    public const NAME = 'orobox-e2e-test-bundle';

    public static function name(): string
    {
        return self::NAME;
    }
}
