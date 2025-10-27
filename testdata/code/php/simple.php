<?php

namespace App\Service;

use App\Model\User;
use App\Repository\RepositoryInterface;

const API_KEY = "test-api-key";
const MAX_RETRIES = 3;

class UserService
{
    private const DEFAULT_LIMIT = 100;

    private array $users = [];
    private int $counter = 0;

    public function __construct()
    {
        $this->users = [];
    }

    public function addUser(User $user): void
    {
        if ($user->validate()) {
            $this->users[] = $user;
            $this->counter++;
        }
    }

    public function findById(string $id): ?User
    {
        foreach ($this->users as $user) {
            if ($user->getId() === $id) {
                return $user;
            }
        }
        return null;
    }

    public function getCount(): int
    {
        return count($this->users);
    }
}

class User
{
    private string $id;
    private string $name;
    private string $email;

    public function __construct(string $id, string $name, string $email)
    {
        $this->id = $id;
        $this->name = $name;
        $this->email = $email;
    }

    public function getId(): string
    {
        return $this->id;
    }

    public function getName(): string
    {
        return $this->name;
    }

    public function getEmail(): string
    {
        return $this->email;
    }

    public function validate(): bool
    {
        return str_contains($this->email, '@');
    }
}

interface RepositoryInterface
{
    public function add(mixed $item): void;
    public function findById(string $id): mixed;
}

trait Timestampable
{
    private ?\DateTimeInterface $createdAt = null;

    public function getCreatedAt(): ?\DateTimeInterface
    {
        return $this->createdAt;
    }
}
