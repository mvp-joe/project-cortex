require 'json'
require 'net/http'

API_KEY = "test-api-key"
MAX_RETRIES = 3
DEBUG_MODE = true

$global_counter = 0

module UserManagement
  class User
    attr_reader :id, :name, :email

    def initialize(id, name, email)
      @id = id
      @name = name
      @email = email
    end

    def validate
      @email.include?('@')
    end

    def to_hash
      {
        id: @id,
        name: @name,
        email: @email
      }
    end
  end

  class UserRepository
    def initialize
      @users = []
    end

    def add(user)
      @users << user if user.validate
    end

    def find_by_id(id)
      @users.find { |u| u.id == id }
    end

    def find_by_email(email)
      @users.find { |u| u.email == email }
    end

    def count
      @users.length
    end

    def all
      @users
    end
  end
end

def create_user(id, name, email)
  UserManagement::User.new(id, name, email)
end

def validate_email(email)
  email.match?(/\A[\w+\-.]+@[a-z\d\-]+(\.[a-z\d\-]+)*\.[a-z]+\z/i)
end
