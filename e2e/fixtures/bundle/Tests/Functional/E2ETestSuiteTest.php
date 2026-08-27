<?php

declare(strict_types=1);

namespace Orobox\Bundle\E2ETestBundle\Tests\Functional;

use PHPUnit\Framework\TestCase;

/**
 * The functional suite's only test. It deliberately extends PHPUnit's TestCase rather than Oro's
 * WebTestCase: what the e2e step gates on here is that `orobox test --testsuite functional`
 * reaches PHPUnit and writes a JUnit report for that suite. Whether Oro's own functional harness
 * boots is covered by the project case, which runs OroPlatform's real functional tests against
 * the database `orobox test-init` provisioned.
 */
class E2ETestSuiteTest extends TestCase
{
    public function testTheFunctionalSuiteRuns(): void
    {
        self::assertDirectoryExists(__DIR__);
    }
}
