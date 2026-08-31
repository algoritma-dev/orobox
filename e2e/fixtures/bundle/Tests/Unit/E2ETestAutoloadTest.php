<?php

declare(strict_types=1);

namespace Orobox\Bundle\E2ETestBundle\Tests\Unit;

use Orobox\Bundle\E2ETestBundle\Marker;
use PHPUnit\Framework\TestCase;

/**
 * Asserts that the bundle was installed into the application rather than merely copied next to
 * it: the class is resolved through the application's autoloader, which only knows about it
 * because `orobox init` required the checkout as a path repository.
 */
class E2ETestAutoloadTest extends TestCase
{
    public function testTheBundleNamespaceIsAutoloadable(): void
    {
        self::assertSame(Marker::NAME, Marker::name());
    }
}
