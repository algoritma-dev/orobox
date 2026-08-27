<?php

declare(strict_types=1);

namespace Orobox\Bundle\E2ETestBundle\Tests\Unit;

use Orobox\Bundle\E2ETestBundle\E2ETestBundle;
use PHPUnit\Framework\TestCase;

/**
 * Asserts that the bundle was installed into the application rather than merely copied next to
 * it: the class is resolved through the application's autoloader, which only knows about it
 * because `orobox init` required the checkout as a path repository.
 */
class E2ETestAutoloadTest extends TestCase
{
    public function testTheBundleClassIsAutoloadable(): void
    {
        self::assertTrue(class_exists(E2ETestBundle::class));
    }
}
