import MySQLdb as mdb
from django.conf import settings


class FastcpSqlService(object):
    """FastCP sql service.

    This class handles the interaction with MySQL databases.
    """

    def __init__(self) -> None:
        """We establish the MySQL connection in this method."""
        self.con = mdb.connect(
            host='localhost', user=settings.FASTCP_SQL_USER, passwd=settings.FASTCP_SQL_PASSWORD)

    def _execute_sql(self, sql: str, ret_result: bool = False) -> bool:
        """Execute SQL.

        Executes an SQL statement.

        Args:
            sql (str): The SQL statement.
            ret_result (bool): Should return the result or not.

        Returns:
            bool: True on success and False otherwise.
        """
        try:
            cur = self.con.cursor()
            cur.execute(sql)
            if ret_result:
                return cur.fetchone()
            else:
                return True
        except Exception as e:
            raise e
        finally:
            cur.close()

        return False

    def setup_db(self, user: str, password: str, dbname: str) -> None:
        """Setup DB.

        Creates a MySQL user using the provided username and given password, creates the database, and grants priviliges to
        the user on the created database.

        Args:
            user (str): The username string.
            password (str): The plain text password.
            dbname (str): The database name.

        Returns:
            bool: True on success and False otherwise
        """

        # SQL statement
        res_1 = self._execute_sql(
            f"CREATE USER '{user}'@'localhost' IDENTIFIED BY '{password}'")
        res_2 = self._execute_sql(
            f"CREATE USER '{user}'@'%' IDENTIFIED BY '{password}'")
        res_3 = self._execute_sql(f"CREATE DATABASE {dbname}")
        res_4 = self._execute_sql(
            f"GRANT ALL PRIVILEGES ON {dbname}.* TO '{user}'@'localhost'")
        res_5 = self._execute_sql(
            f"GRANT ALL PRIVILEGES ON {dbname}.* TO '{user}'@'%'")
        res_6 = self._execute_sql("FLUSH PRIVILEGES")
        return all([res_1, res_2, res_3, res_4, res_5, res_6])

    def update_password(self, username: str, password: str) -> bool:
        """Update a user's password.
        
        Args:
            username (str): MySQL username.
            password (str): New password for the user.
            
        Returns:
            bool: True on success False otherwise.
        """
        res_1 = self._execute_sql(f"ALTER USER '{username}@'%' IDENTIFIED BY '{password}'")
        res_2 = self._execute_sql(f"ALTER USER '{username}@'localhost' IDENTIFIED BY '{password}'")
        return all([res_1, res_2])

    def drop_db(self, dbname: str) -> bool:
        """Drops the database"""
        return self._execute_sql(f"DROP DATABASE {dbname}")

    
    def drop_user(self, user: str) -> bool:
        """Drops the user"""
        res_1 = self._execute_sql(f"DROP USER '{user}'@'localhost'")
        res_2 = self._execute_sql(f"DROP USER '{user}'@'%'")
        return all([res_1, res_2])
